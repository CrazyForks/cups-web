package main

import (
	"bufio"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"

	"github.com/phpdave11/gofpdf"
)

const pdfPageMarginMM = 10.0

// paperSizeToGofpdf 将纸张大小名称映射到 gofpdf 参数
// 返回：gofpdf 认识的标准名称（或空字符串表示自定义）、自定义尺寸（如果是自定义纸张）
func paperSizeToGofpdf(size string) (string, gofpdf.SizeType) {
	switch size {
	case "A5":
		return "A5", gofpdf.SizeType{}
	case "A4":
		return "A4", gofpdf.SizeType{}
	case "A3":
		return "A3", gofpdf.SizeType{}
	case "A2":
		return "A2", gofpdf.SizeType{}
	case "A1":
		return "A1", gofpdf.SizeType{}
	case "Letter":
		return "Letter", gofpdf.SizeType{}
	case "Legal":
		return "Legal", gofpdf.SizeType{}
	case "5inch":
		// 89×127 mm
		return "", gofpdf.SizeType{Wd: 89, Ht: 127}
	case "6inch":
		// 102×152 mm
		return "", gofpdf.SizeType{Wd: 102, Ht: 152}
	case "7inch":
		// 127×178 mm
		return "", gofpdf.SizeType{Wd: 127, Ht: 178}
	case "8inch":
		// 152×203 mm
		return "", gofpdf.SizeType{Wd: 152, Ht: 203}
	case "10inch":
		// 203×254 mm
		return "", gofpdf.SizeType{Wd: 203, Ht: 254}
	default:
		// 默认使用 A4
		return "A4", gofpdf.SizeType{}
	}
}

// getOrientationCode 将方向字符串转换为 gofpdf 方向代码
func getOrientationCode(orientation string) string {
	if orientation == "landscape" {
		return "L"
	}
	// 默认纵向
	return "P"
}

func convertImageToPDF(inputPath string, orientation string, paperSize string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "convert-img-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	f, err := os.Open(inputPath)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	cfg, _, err := image.DecodeConfig(f)
	_ = f.Close()
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		cleanup()
		return "", nil, errors.New("invalid image dimensions")
	}

	// 处理方向和纸张大小
	orientCode := getOrientationCode(orientation)
	paperName, customSize := paperSizeToGofpdf(paperSize)

	var pdf *gofpdf.Fpdf
	if paperName != "" {
		// 使用标准纸张
		pdf = gofpdf.New(orientCode, "mm", paperName, "")
	} else {
		// 使用自定义尺寸
		pdf = gofpdf.NewCustom(&gofpdf.InitType{
			UnitStr:    "mm",
			Size:       customSize,
			OrientationStr: orientCode,
		})
	}

	pdf.SetMargins(pdfPageMarginMM, pdfPageMarginMM, pdfPageMarginMM)
	pdf.SetAutoPageBreak(false, pdfPageMarginMM)
	pdf.AddPage()

	pageW, pageH := pdf.GetPageSize()
	maxW := pageW - 2*pdfPageMarginMM
	maxH := pageH - 2*pdfPageMarginMM
	scale := math.Min(maxW/float64(cfg.Width), maxH/float64(cfg.Height))
	if scale <= 0 {
		scale = 1
	}
	w := float64(cfg.Width) * scale
	h := float64(cfg.Height) * scale
	x := (pageW - w) / 2
	y := (pageH - h) / 2

	opts := gofpdf.ImageOptions{ImageType: "", ReadDpi: true}
	pdf.ImageOptions(inputPath, x, y, w, h, false, opts, 0, "")

	outPath := filepath.Join(tmpDir, "image.pdf")
	if err := pdf.OutputFileAndClose(outPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return outPath, cleanup, nil
}

func convertTextToPDF(inputPath string, orientation string, paperSize string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "convert-text-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	f, err := os.Open(inputPath)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	defer f.Close()

	// 处理方向和纸张大小
	orientCode := getOrientationCode(orientation)
	paperName, customSize := paperSizeToGofpdf(paperSize)

	var pdf *gofpdf.Fpdf
	if paperName != "" {
		// 使用标准纸张
		pdf = gofpdf.New(orientCode, "mm", paperName, "")
	} else {
		// 使用自定义尺寸
		pdf = gofpdf.NewCustom(&gofpdf.InitType{
			UnitStr:        "mm",
			Size:           customSize,
			OrientationStr: orientCode,
		})
	}

	pdf.SetMargins(pdfPageMarginMM, pdfPageMarginMM, pdfPageMarginMM)
	pdf.SetAutoPageBreak(false, pdfPageMarginMM)
	pdf.AddPage()
	if err := setPdfTextFont(pdf, 10); err != nil {
		cleanup()
		return "", nil, err
	}

	_, pageH := pdf.GetPageSize()
	lineHeight := (pageH - 2*pdfPageMarginMM) / float64(textLinesPerPage)
	if lineHeight <= 0 {
		lineHeight = 4
	}

	pageW, _ := pdf.GetPageSize()
	cellW := pageW - 2*pdfPageMarginMM

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineIndex := 0
	for scanner.Scan() {
		if lineIndex >= textLinesPerPage {
			pdf.AddPage()
			lineIndex = 0
		}
		y := pdfPageMarginMM + lineHeight*float64(lineIndex)
		pdf.SetXY(pdfPageMarginMM, y)
		pdf.CellFormat(cellW, lineHeight, scanner.Text(), "", 0, "LM", false, 0, "")
		lineIndex++
	}
	if err := scanner.Err(); err != nil {
		cleanup()
		return "", nil, err
	}

	outPath := filepath.Join(tmpDir, "text.pdf")
	if err := pdf.OutputFileAndClose(outPath); err != nil {
		cleanup()
		return "", nil, err
	}
	return outPath, cleanup, nil
}
