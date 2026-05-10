package main

type analogCmd struct {
	Read    analogReadCmd    `cmd:"" help:"Read analog inputs."`
	Scan    analogScanCmd    `cmd:"" help:"Continuous analog input scan."`
	Out     analogOutCmd     `cmd:"" help:"Write analog output."`
	OutScan analogOutScanCmd `cmd:"out-scan" help:"Continuous analog output scan."`
}
