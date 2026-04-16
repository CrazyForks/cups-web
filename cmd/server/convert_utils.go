package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func convertOfficeToPDF(ctx context.Context, inputPath string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "convert-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	cmd := exec.CommandContext(ctx, "libreoffice", "--headless", "--convert-to", "pdf", "--outdir", tmpDir, inputPath)
	cmd.Env = append(os.Environ(), "LANG=zh_CN.UTF-8", "LC_ALL=zh_CN.UTF-8")
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("conversion failed: %w - %s", err, string(out))
	}

	base := filepath.Base(inputPath)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	outPath := filepath.Join(tmpDir, name+".pdf")
	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		matches, _ := filepath.Glob(filepath.Join(tmpDir, "*.pdf"))
		if len(matches) == 0 {
			cleanup()
			return "", nil, fmt.Errorf("conversion produced no PDF")
		}
		outPath = matches[0]
	}

	return outPath, cleanup, nil
}

func convertOFDToPDF(ctx context.Context, inputPath string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "convert-ofd-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmpDir) }

	outPath := filepath.Join(tmpDir, "output.pdf")

	jarPath := os.Getenv("OFD_CONVERTER_JAR")
	if jarPath == "" {
		jarPath = "/ofd-converter.jar"
	}

	cmd := exec.CommandContext(ctx, "java", "-Xmx512m", "-jar", jarPath, inputPath, outPath)
	cmd.Env = append(os.Environ(), "LANG=zh_CN.UTF-8", "LC_ALL=zh_CN.UTF-8")
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("OFD to PDF conversion failed: %w - %s", err, string(out))
	}

	if _, err := os.Stat(outPath); os.IsNotExist(err) {
		cleanup()
		return "", nil, fmt.Errorf("OFD to PDF conversion produced no output")
	}

	return outPath, cleanup, nil
}

func convertTimeoutContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, 60*time.Second)
}
