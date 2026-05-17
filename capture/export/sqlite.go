package export

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/borud/mcc-usb-1808/v4/capture"
	_ "modernc.org/sqlite" // needed for SQLite support
)

const sqliteBatchSize = 1000

// SQLite writes all remaining frames from r to a SQLite database at path.
//
// Three tables are created:
//
//	metadata  — key-value pairs of capture metadata and properties.
//	channels  — one row per channel with index, type, range, name, and calibration.
//	frames    — one row per frame with timestamp_s and one column per channel.
//
// Frame columns are named after the channels (see [capture.Channel.Name]),
// falling back to "ch0", "ch1", etc. Inserts are batched in transactions
// for performance.
func SQLite(path string, r *capture.Reader) error {
	h := r.Header()
	cols := columnNames(h.Channels)

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()

	// Enable WAL mode for write performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return err
	}

	if err := createTables(db, cols); err != nil {
		return err
	}
	if err := insertMetadata(db, h); err != nil {
		return err
	}
	if err := insertChannels(db, h); err != nil {
		return err
	}
	return insertFrames(db, r, h, cols)
}

func createTables(db *sql.DB, cols []string) error {
	// metadata
	if _, err := db.Exec(`CREATE TABLE metadata (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		return err
	}

	// channels
	if _, err := db.Exec(`CREATE TABLE channels (
		position      INTEGER PRIMARY KEY,
		channel_index INTEGER NOT NULL,
		type          INTEGER NOT NULL,
		range_code    INTEGER NOT NULL,
		name          TEXT NOT NULL,
		cal_slope     REAL,
		cal_offset    REAL
	)`); err != nil {
		return err
	}

	// frames — dynamic columns
	colDefs := make([]string, len(cols))
	for i, name := range cols {
		colDefs[i] = fmt.Sprintf(`"%s" REAL`, name)
	}
	createSQL := fmt.Sprintf(`CREATE TABLE frames (
		frame_id    INTEGER PRIMARY KEY,
		timestamp_s REAL NOT NULL,
		%s
	)`, strings.Join(colDefs, ",\n\t\t"))

	_, err := db.Exec(createSQL)
	return err
}

func insertMetadata(db *sql.DB, h capture.Header) error {
	stmt, err := db.Prepare("INSERT INTO metadata (key, value) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	insert := func(key, value string) error {
		if value == "" {
			return nil
		}
		_, err := stmt.Exec(key, value)
		return err
	}

	if err := insert("device_model", h.DeviceModel); err != nil {
		return err
	}
	if err := insert("device_serial", h.DeviceSerial); err != nil {
		return err
	}
	if err := insert("fpga_version", h.FPGAVersion); err != nil {
		return err
	}
	if !h.CalibrationDate.IsZero() {
		if err := insert("calibration_date", h.CalibrationDate.Format("2006-01-02")); err != nil {
			return err
		}
	}
	if err := insert("sample_rate", strconv.FormatFloat(h.SampleRate, 'f', -1, 64)); err != nil {
		return err
	}
	if h.FrameCount > 0 {
		if err := insert("frame_count", strconv.FormatUint(h.FrameCount, 10)); err != nil {
			return err
		}
	}
	if h.Timestamp > 0 {
		if err := insert("timestamp", strconv.FormatInt(h.Timestamp, 10)); err != nil {
			return err
		}
	}
	if err := insert("application_name", h.ApplicationName); err != nil {
		return err
	}
	if err := insert("session_id", h.SessionID); err != nil {
		return err
	}
	if err := insert("description", h.Description); err != nil {
		return err
	}
	if err := insert("operator", h.Operator); err != nil {
		return err
	}
	for k, v := range h.Properties {
		if err := insert("property."+k, v); err != nil {
			return err
		}
	}
	return nil
}

func insertChannels(db *sql.DB, h capture.Header) error {
	stmt, err := db.Prepare(`INSERT INTO channels
		(position, channel_index, type, range_code, name, cal_slope, cal_offset)
		VALUES (?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, ch := range h.Channels {
		name := ch.Name
		if name == "" {
			name = fmt.Sprintf("ch%d", i)
		}
		var slope, offset *float64
		if ch.Cal != nil {
			s := float64(ch.Cal.Slope)
			o := float64(ch.Cal.Offset)
			slope = &s
			offset = &o
		}
		if _, err := stmt.Exec(i, ch.Index, int(ch.Type), int(ch.Range), name, slope, offset); err != nil {
			return err
		}
	}
	return nil
}

func insertFrames(db *sql.DB, r *capture.Reader, h capture.Header, cols []string) error {
	numCh := len(cols)

	// Build parameterized INSERT.
	quotedCols := make([]string, numCh)
	for i, name := range cols {
		quotedCols[i] = fmt.Sprintf(`"%s"`, name)
	}
	placeholders := make([]string, numCh+2)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := fmt.Sprintf(`INSERT INTO frames (frame_id, timestamp_s, %s) VALUES (%s)`, // #nosec G201 -- column names from internal capture metadata, not user input
		strings.Join(quotedCols, ", "),
		strings.Join(placeholders, ", "))

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()

	args := make([]any, numCh+2)
	frameIdx := 0
	batchCount := 0

	for frame, fErr := range r.Frames() {
		if fErr != nil {
			return fErr
		}

		args[0] = frameIdx
		args[1] = float64(frameIdx) / h.SampleRate
		vals := frame.Values()
		for i, v := range vals {
			args[i+2] = v
		}

		if _, err := stmt.Exec(args...); err != nil {
			return err
		}

		frameIdx++
		batchCount++

		if batchCount >= sqliteBatchSize {
			stmt.Close()
			if err := tx.Commit(); err != nil {
				return err
			}
			tx, err = db.Begin()
			if err != nil {
				return err
			}
			stmt, err = tx.Prepare(insertSQL)
			if err != nil {
				return err
			}
			batchCount = 0
		}
	}

	stmt.Close()
	return tx.Commit()
}
