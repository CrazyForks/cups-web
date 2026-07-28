package main

import (
	"regexp"
	"strings"
)

// DriverMeta holds static metadata about a known printer driver.
type DriverMeta struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName"`
	Description   string   `json:"description"`
	Arch          []string `json:"arch"`
	NeedCompile   bool     `json:"needCompile"`
	MatchPatterns []string `json:"-"` // not exposed to API
}

// DriverStatus extends DriverMeta with installation state.
type DriverStatus struct {
	DriverMeta
	Installed   bool   `json:"installed"`
	InstalledAt string `json:"installedAt,omitempty"`
}

// DetectedPrinter represents a printer discovered via lpinfo.
type DetectedPrinter struct {
	DeviceURI    string      `json:"deviceUri"`
	Manufacturer string      `json:"manufacturer"`
	Model        string      `json:"model"`
	Connection   string      `json:"connection"` // "usb", "network", "direct"
	DriverMatch  *DriverMeta `json:"driverMatch"`
	HasDriver    bool        `json:"hasDriver"` // CUPS already has a matching PPD
}

var driversRegistry = []DriverMeta{
	{
		Name: "canon-ufr2", DisplayName: "Canon UFR II",
		Description: "Canon imageCLASS / i-SENSYS / imageRUNNER 系列激光打印机",
		Arch: []string{"amd64", "arm64"}, NeedCompile: false,
		MatchPatterns: []string{`Canon iR`, `Canon imageCLASS`, `Canon i-SENSYS`, `Canon MF`, `Canon LBP[3-9]`, `Canon imageRUNNER`},
	},
	{
		Name: "canon-capt", DisplayName: "Canon CAPT",
		Description: "Canon LBP2900 / LBP2900B",
		Arch: []string{"all"}, NeedCompile: true,
		MatchPatterns: []string{`Canon LBP2900`, `Canon LBP-2900`},
	},
	{
		Name: "hp-laserjet1020", DisplayName: "HP LaserJet 1020",
		Description: "HP LaserJet 1020 固件 + A4 默认 PPD",
		Arch: []string{"all"}, NeedCompile: false,
		MatchPatterns: []string{`HP LaserJet 1020`},
	},
	{
		Name: "foo2zjs-firmware", DisplayName: "HP foo2zjs Firmware",
		Description: "HP LaserJet 1000/1005/1018/P1005/P1006/P1505 固件",
		Arch: []string{"all"}, NeedCompile: true,
		MatchPatterns: []string{`HP LaserJet 1000`, `HP LaserJet 1005`, `HP LaserJet 1018`, `HP LaserJet P100`, `HP LaserJet P1505`},
	},
	{
		Name: "escpr2", DisplayName: "Epson ESC/P-R 2",
		Description: "Epson ET-18100, L8050, L8160, WF-7840 等新款喷墨打印机",
		Arch: []string{"amd64", "armhf", "arm64"}, NeedCompile: false,
		MatchPatterns: []string{`Epson ET-`, `Epson L[0-9]`, `Epson WF-`, `Epson XP-`},
	},
	{
		Name: "epson-cn", DisplayName: "Epson 国行驱动",
		Description: "Epson L380, L455 等国行喷墨打印机",
		Arch: []string{"amd64"}, NeedCompile: false,
		MatchPatterns: []string{`Epson L380`, `Epson L455`},
	},
	{
		Name: "konica-bizhub", DisplayName: "Konica Minolta bizhub",
		Description: "Konica Minolta bizhub 3000MF 黑白激光打印机",
		Arch: []string{"amd64", "arm64"}, NeedCompile: false,
		MatchPatterns: []string{`KONICA MINOLTA`, `Konica Minolta`, `bizhub`},
	},
	{
		Name: "sharp", DisplayName: "Sharp PostScript",
		Description: "Sharp MX-C2622R 等 PostScript 打印机",
		Arch: []string{"all"}, NeedCompile: false,
		MatchPatterns: []string{`Sharp MX`, `SHARP MX`},
	},
	{
		Name: "gutenprint", DisplayName: "Gutenprint",
		Description: "Gutenprint 高质量打印驱动（支持大量打印机型号）",
		Arch: []string{"amd64", "arm64"}, NeedCompile: false,
		MatchPatterns: []string{}, // Generic, matches many printers
	},
}

// matchDriverForPrinter finds the best matching driver for a printer model string.
func matchDriverForPrinter(modelStr string) *DriverMeta {
	upper := strings.ToUpper(modelStr)
	for i := range driversRegistry {
		d := &driversRegistry[i]
		for _, pattern := range d.MatchPatterns {
			re, err := regexp.Compile("(?i)" + pattern)
			if err != nil {
				// Fall back to simple substring match if regex is invalid.
				if strings.Contains(upper, strings.ToUpper(pattern)) {
					return d
				}
				continue
			}
			if re.MatchString(modelStr) {
				return d
			}
		}
	}
	return nil
}

// findDriverByName looks up a driver by its canonical name.
func findDriverByName(name string) *DriverMeta {
	for i := range driversRegistry {
		if driversRegistry[i].Name == name {
			return &driversRegistry[i]
		}
	}
	return nil
}
