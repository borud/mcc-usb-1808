package export

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/borud/mcc-usb-1808/v3/capture"
	pq "github.com/parquet-go/parquet-go"
)

const (
	defaultParquetBatchSize       = 1024
	defaultParquetRowsPerRowGroup = 128 * 1024
	parquetMetadataKey            = "mcc-usb-1808.capture"
	parquetMetadataSchemaVersion  = 1
	parquetColumnRoleFrameID      = "frame_id"
	parquetColumnRoleTimestamp    = "timestamp_s"
	parquetColumnRoleValue        = "value"
	parquetColumnRoleRaw          = "raw"
	parquetTimestampColumnName    = "timestamp_s"
	parquetFrameIDColumnName      = "frame_id"
)

// ParquetOption configures Parquet export.
type ParquetOption func(*parquetConfig)

type parquetConfig struct {
	includeRaw bool
}

// WithRaw includes raw uint32 sample columns in Parquet output.
//
// Raw columns are available for RawUint32 captures and are written alongside
// the calibrated value columns. The raw columns preserve the exact stored
// device words for downstream processing.
func WithRaw() ParquetOption {
	return func(c *parquetConfig) {
		c.includeRaw = true
	}
}

// Parquet writes all remaining frames from r as an Apache Parquet file to w.
//
// The default export contains frame_id, timestamp_s, and one calibrated value
// column per capture channel. Use [WithRaw] to add one uint32 raw column per
// channel for RawUint32 captures. Capture metadata and the channel-to-column
// mapping are stored in Parquet key/value metadata under "mcc-usb-1808.capture".
func Parquet(w io.Writer, r *capture.Reader, opts ...ParquetOption) error {
	h := r.Header()

	var cfg parquetConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	includeRaw := cfg.includeRaw
	if includeRaw && h.Format != capture.RawUint32 {
		return fmt.Errorf("raw parquet columns require RawUint32 capture: %w", capture.ErrInvalidFormat)
	}

	schema, columns, err := parquetSchema(h, includeRaw)
	if err != nil {
		return err
	}

	writerOpts := []pq.WriterOption{
		schema,
		pq.MaxRowsPerRowGroup(defaultParquetRowsPerRowGroup),
		pq.Compression(&pq.Zstd),
	}

	pw := pq.NewGenericWriter[any](w, writerOpts...)
	builder := pq.NewRowBuilder(schema)
	batch := make([]pq.Row, 0, defaultParquetBatchSize)

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if _, err := pw.WriteRows(batch); err != nil {
			return err
		}
		for i := range batch {
			batch[i] = nil
		}
		batch = batch[:0]
		return nil
	}

	var frameIdx uint64
	for frame, frameErr := range r.Frames() {
		if frameErr != nil {
			_ = pw.Close()
			return frameErr
		}
		if frameIdx > math.MaxInt64 {
			_ = pw.Close()
			return fmt.Errorf("frame index %d overflows parquet int64 frame_id", frameIdx)
		}

		builder.Reset()
		builder.Add(columns[0].columnIndex, pq.Int64Value(int64(frameIdx)))
		builder.Add(columns[1].columnIndex, pq.DoubleValue(h.TimeAtFrame(frameIdx)))

		values := frame.Values()
		raw := frame.RawValues()
		for _, col := range columns[2:] {
			switch col.Role {
			case parquetColumnRoleValue:
				builder.Add(col.columnIndex, pq.DoubleValue(values[col.ChannelPosition]))
			case parquetColumnRoleRaw:
				builder.Add(col.columnIndex, pq.ValueOf(raw[col.ChannelPosition]))
			}
		}

		batch = append(batch, builder.Row())
		if len(batch) >= defaultParquetBatchSize {
			if err := flush(); err != nil {
				_ = pw.Close()
				return err
			}
		}
		frameIdx++
	}

	if err := flush(); err != nil {
		_ = pw.Close()
		return err
	}
	metadata, err := parquetMetadata(h, columns, includeRaw, frameIdx)
	if err != nil {
		_ = pw.Close()
		return err
	}
	pw.SetKeyValueMetadata(parquetMetadataKey, metadata)
	return pw.Close()
}

type parquetColumn struct {
	Name            string              `json:"name"`
	Role            string              `json:"role"`
	ChannelPosition int                 `json:"channel_position"`
	ChannelIndex    int                 `json:"channel_index"`
	ChannelType     capture.ChannelType `json:"channel_type"`
	OriginalName    string              `json:"original_name,omitempty"`
	Unit            string              `json:"unit,omitempty"`
	columnIndex     int
}

type parquetFileMetadata struct {
	Version            int             `json:"version"`
	HeaderFrameCount   uint64          `json:"header_frame_count,omitempty"`
	ExportedFrameCount uint64          `json:"exported_frame_count"`
	RawIncluded        bool            `json:"raw_included"`
	Header             capture.Header  `json:"header"`
	Columns            []parquetColumn `json:"columns"`
}

func parquetSchema(h capture.Header, includeRaw bool) (*pq.Schema, []parquetColumn, error) {
	fields := pq.Group{}
	used := map[string]bool{}

	addField := func(name string, node pq.Node, col parquetColumn) parquetColumn {
		name = uniqueParquetColumnName(name, used)
		fields[name] = node
		col.Name = name
		return col
	}

	columns := []parquetColumn{
		addField(parquetFrameIDColumnName, pq.Int(64), parquetColumn{
			Role:            parquetColumnRoleFrameID,
			ChannelPosition: -1,
		}),
		addField(parquetTimestampColumnName, pq.Leaf(pq.DoubleType), parquetColumn{
			Role:            parquetColumnRoleTimestamp,
			ChannelPosition: -1,
			Unit:            "s",
		}),
	}

	names := columnNames(h.Channels)
	for i, ch := range h.Channels {
		unit := ""
		if ch.Type == capture.AnalogIn {
			unit = "V"
		}
		columns = append(columns, addField(names[i], pq.Leaf(pq.DoubleType), parquetColumn{
			Role:            parquetColumnRoleValue,
			ChannelPosition: i,
			ChannelIndex:    ch.Index,
			ChannelType:     ch.Type,
			OriginalName:    ch.Name,
			Unit:            unit,
		}))
		if includeRaw {
			columns = append(columns, addField(names[i]+"_raw", pq.Uint(32), parquetColumn{
				Role:            parquetColumnRoleRaw,
				ChannelPosition: i,
				ChannelIndex:    ch.Index,
				ChannelType:     ch.Type,
				OriginalName:    ch.Name,
				Unit:            "raw",
			}))
		}
	}

	schema := pq.NewSchema("mcc_usb_1808_capture", fields)
	columnIndexes := make(map[string]int, len(schema.Columns()))
	for i, path := range schema.Columns() {
		if len(path) == 1 {
			columnIndexes[path[0]] = i
		}
	}
	for i := range columns {
		index, ok := columnIndexes[columns[i].Name]
		if !ok {
			return nil, nil, fmt.Errorf("parquet schema missing column %q", columns[i].Name)
		}
		columns[i].columnIndex = index
	}

	return schema, columns, nil
}

func parquetMetadata(h capture.Header, columns []parquetColumn, includeRaw bool, exportedFrameCount uint64) (string, error) {
	meta := parquetFileMetadata{
		Version:            parquetMetadataSchemaVersion,
		HeaderFrameCount:   h.FrameCount,
		ExportedFrameCount: exportedFrameCount,
		RawIncluded:        includeRaw,
		Header:             h,
		Columns:            columns,
	}
	buf, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

func uniqueParquetColumnName(name string, used map[string]bool) string {
	name = sanitizeParquetColumnName(name)
	base := name
	for n := 2; used[name]; n++ {
		name = fmt.Sprintf("%s_%d", base, n)
	}
	used[name] = true
	return name
}

func sanitizeParquetColumnName(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		out = "field"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
	}
	return out
}
