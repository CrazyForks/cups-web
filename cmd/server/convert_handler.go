package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
)

func convertHandler(w http.ResponseWriter, r *http.Request) {
	// Expect multipart form
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}
	file, fh, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 读取方向和纸张大小参数
	orientation := r.FormValue("orientation")
	paperSize := r.FormValue("paper_size")

	inPath, cleanup, err := saveTempUpload(file, fh.Filename)
	if err != nil {
		http.Error(w, "failed to save file", http.StatusInternalServerError)
		return
	}
	defer cleanup()

	ctx, cancel := convertTimeoutContext(r.Context())
	defer cancel()

	kind := detectFileKind(inPath, fh.Filename)
	var outPath string
	var outCleanup func()
	if kind == fileKindText {
		outPath, outCleanup, err = convertTextToPDF(inPath, orientation, paperSize)
	} else if kind == fileKindOFD {
		outPath, outCleanup, err = convertOFDToPDF(ctx, inPath)
	} else {
		outPath, outCleanup, err = convertOfficeToPDF(ctx, inPath)
	}
	if err != nil {
		http.Error(w, "conversion failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer outCleanup()

	base := filepath.Base(fh.Filename)
	ext := filepath.Ext(base)
	name := base[0 : len(base)-len(ext)]

	// Stream PDF back
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+".pdf\"")
	pdfFile, err := os.Open(outPath)
	if err != nil {
		http.Error(w, "failed to open converted file", http.StatusInternalServerError)
		return
	}
	defer pdfFile.Close()
	if _, err := io.Copy(w, pdfFile); err != nil {
		// nothing more we can do
		return
	}
}
