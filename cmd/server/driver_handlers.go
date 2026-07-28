package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	driversDataDir   = "/opt/cups-drivers/data"
	driversScriptDir = "/opt/cups-drivers/scripts"
)

// GET /api/admin/drivers — list all drivers with installation status.
func adminListDriversHandler(w http.ResponseWriter, r *http.Request) {
	var result []DriverStatus
	for _, d := range driversRegistry {
		status := DriverStatus{DriverMeta: d}
		manifestPath := filepath.Join(driversDataDir, d.Name, "manifest.txt")
		if info, err := os.Stat(manifestPath); err == nil {
			status.Installed = true
			status.InstalledAt = info.ModTime().Format(time.RFC3339)
		}
		// Also check metadata.txt for an explicit install date.
		metaPath := filepath.Join(driversDataDir, d.Name, "metadata.txt")
		if data, err := os.ReadFile(metaPath); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(line, "date=") {
					status.InstalledAt = strings.TrimPrefix(line, "date=")
				}
			}
		}
		result = append(result, status)
	}
	writeJSON(w, result)
}

// POST /api/admin/drivers/install — install a driver by name.
func adminInstallDriverHandler(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if payload.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "driver name is required")
		return
	}
	if findDriverByName(payload.Name) == nil {
		writeJSONError(w, http.StatusNotFound, "unknown driver: "+payload.Name)
		return
	}

	// Run driver-install synchronously (timeout governed by request context).
	cmd := exec.CommandContext(r.Context(), "/usr/local/bin/driver-install", payload.Name)
	cmd.Env = append(os.Environ(), "CUPS_AIO=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[driver-install] %s failed: %v\n%s", payload.Name, err, string(output))
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("driver installation failed: %v", err))
		return
	}

	log.Printf("[driver-install] %s installed successfully", payload.Name)
	writeJSON(w, map[string]any{"ok": true, "name": payload.Name, "log": string(output)})
}

// POST /api/admin/drivers/remove — remove a driver by name.
func adminRemoveDriverHandler(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if payload.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "driver name is required")
		return
	}

	cmd := exec.CommandContext(r.Context(), "/usr/local/bin/driver-remove", payload.Name)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[driver-remove] %s failed: %v\n%s", payload.Name, err, string(output))
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("driver removal failed: %v", err))
		return
	}

	writeJSON(w, map[string]any{"ok": true, "name": payload.Name})
}

// GET /api/admin/drivers/detect — detect connected printers and recommend drivers.
func adminDetectPrintersHandler(w http.ResponseWriter, r *http.Request) {
	// Run lpinfo -v to discover connected devices.
	cmd := exec.CommandContext(r.Context(), "lpinfo", "-v")
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[driver-detect] lpinfo -v failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "failed to detect printers")
		return
	}

	// Parse lpinfo -v output.
	// Format: <type> <uri> "<description>" "<make-and-model>"
	// Example: direct usb://Canon/LBP2900?serial=XXX "Canon LBP2900" "Canon LBP2900"
	var printers []DetectedPrinter
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		connType := parts[0] // "direct", "network", "serial", etc.
		uri := parts[1]

		// Determine connection type.
		connection := connType
		if strings.Contains(uri, "usb://") {
			connection = "usb"
		} else if strings.Contains(uri, "socket://") || strings.Contains(uri, "lpd://") ||
			strings.Contains(uri, "ipp://") || strings.Contains(uri, "ipps://") ||
			strings.Contains(uri, "http://") || strings.Contains(uri, "https://") {
			connection = "network"
		}

		// Extract manufacturer and model from URI.
		manufacturer, model := parseDeviceURI(uri)

		// Also try to get from the description fields (quoted strings).
		if manufacturer == "" || model == "" {
			desc := ""
			if len(parts) >= 3 {
				desc = parts[2]
			}
			if m, mo := parseDescription(desc); manufacturer == "" {
				manufacturer = m
				model = mo
			}
		}

		// Skip CUPS-PDF, braille, and other virtual printers.
		if strings.Contains(uri, "cups-pdf") || uri == "file:///dev/null" {
			continue
		}
		if strings.Contains(uri, "cups-brf") {
			continue
		}

		printer := DetectedPrinter{
			DeviceURI:    uri,
			Manufacturer: manufacturer,
			Model:        model,
			Connection:   connection,
		}

		// Match against driver registry.
		searchStr := manufacturer + " " + model
		if match := matchDriverForPrinter(searchStr); match != nil {
			printer.DriverMatch = match
		}

		// Check if CUPS already has a matching PPD.
		printer.HasDriver = checkHasDriver(r.Context(), searchStr)

		printers = append(printers, printer)
	}

	writeJSON(w, printers)
}

// POST /api/admin/drivers/setup — install driver + add printer to CUPS in one step.
func adminSetupPrinterHandler(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		DeviceURI  string `json:"deviceUri"`
		DriverName string `json:"driverName"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if payload.DeviceURI == "" {
		writeJSONError(w, http.StatusBadRequest, "deviceUri is required")
		return
	}

	ctx := r.Context()
	driverInstalled := false

	// Step 1: Install driver if specified and not already installed.
	if payload.DriverName != "" {
		manifestPath := filepath.Join(driversDataDir, payload.DriverName, "manifest.txt")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			cmd := exec.CommandContext(ctx, "/usr/local/bin/driver-install", payload.DriverName)
			cmd.Env = append(os.Environ(), "CUPS_AIO=1")
			if output, err := cmd.CombinedOutput(); err != nil {
				log.Printf("[driver-setup] install %s failed: %v\n%s", payload.DriverName, err, string(output))
				writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("driver installation failed: %v", err))
				return
			}
			driverInstalled = true
		}
	}

	// Step 2: Find best PPD for the printer.
	_, model := parseDeviceURI(payload.DeviceURI)
	ppdURI := findBestPPD(ctx, model)

	// Step 3: Generate printer name from model.
	printerName := sanitizePrinterName(model)
	if printerName == "" {
		printerName = "Printer"
	}

	// Step 4: Add printer with lpadmin.
	args := []string{"-p", printerName, "-E", "-v", payload.DeviceURI}
	if ppdURI != "" {
		args = append(args, "-m", ppdURI)
	}
	cmd := exec.CommandContext(ctx, "lpadmin", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[driver-setup] lpadmin failed: %v\n%s", err, string(output))
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to add printer: %v", err))
		return
	}

	// Step 5: Set default paper to A4.
	cmd = exec.CommandContext(ctx, "lpadmin", "-p", printerName, "-o", "media=iso_a4_210x297mm")
	if output, err := cmd.CombinedOutput(); err != nil {
		log.Printf("[driver-setup] set A4 default failed (non-fatal): %v\n%s", err, string(output))
	}

	writeJSON(w, map[string]any{
		"ok":              true,
		"printerName":     printerName,
		"driverInstalled": driverInstalled,
		"ppdUsed":         ppdURI,
	})
}

// POST /api/admin/drivers/upload — upload a custom PPD or .deb package.
func adminUploadDriverHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(50 << 20); err != nil { // 50 MB limit
		writeJSONError(w, http.StatusBadRequest, "file too large or invalid form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "file is required")
		return
	}
	defer file.Close()

	filename := header.Filename
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".ppd":
		// Install PPD file.
		ppdDir := "/usr/share/cups/model/custom"
		os.MkdirAll(ppdDir, 0755)
		destPath := filepath.Join(ppdDir, filename)

		// Validate PPD content.
		content, err := io.ReadAll(file)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to read file")
			return
		}
		n := len(content)
		if n > 256 {
			n = 256
		}
		if !strings.Contains(string(content[:n]), "*PPD-Adobe") {
			writeJSONError(w, http.StatusBadRequest, "invalid PPD file (missing *PPD-Adobe header)")
			return
		}

		if err := os.WriteFile(destPath, content, 0644); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to save PPD")
			return
		}

		// Persist to driver data so it survives container restarts.
		persistDir := filepath.Join(driversDataDir, "custom-ppd", "usr/share/cups/model/custom")
		os.MkdirAll(persistDir, 0755)
		os.WriteFile(filepath.Join(persistDir, filename), content, 0644)

		// Update manifest.
		manifestDir := filepath.Join(driversDataDir, "custom-ppd")
		manifestPath := filepath.Join(manifestDir, "manifest.txt")
		f, _ := os.OpenFile(manifestPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if f != nil {
			fmt.Fprintf(f, "/usr/share/cups/model/custom/%s\n", filename)
			f.Close()
		}

		log.Printf("[driver-upload] installed PPD: %s", filename)
		writeJSON(w, map[string]any{"ok": true, "type": "ppd", "filename": filename})

	case ".deb":
		// Save and install deb package.
		tmpFile, err := os.CreateTemp("/tmp", "driver-upload-*.deb")
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "failed to create temp file")
			return
		}
		tmpPath := tmpFile.Name()
		defer os.Remove(tmpPath)

		if _, err := io.Copy(tmpFile, file); err != nil {
			tmpFile.Close()
			writeJSONError(w, http.StatusInternalServerError, "failed to save file")
			return
		}
		tmpFile.Close()

		cmd := exec.CommandContext(r.Context(), "dpkg", "-i", tmpPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Try apt-get -f install to fix dependencies.
			fixCmd := exec.CommandContext(r.Context(), "apt-get", "install", "-y", "-f", "--no-install-recommends")
			fixCmd.CombinedOutput()

			log.Printf("[driver-upload] dpkg -i failed: %v\n%s", err, string(output))
			writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("package installation failed: %v", err))
			return
		}

		log.Printf("[driver-upload] installed deb: %s", filename)
		writeJSON(w, map[string]any{"ok": true, "type": "deb", "filename": filename})

	default:
		writeJSONError(w, http.StatusBadRequest, "unsupported file type (use .ppd or .deb)")
	}
}

// ── Helper functions ──────────────────────────────────────────────────────────

// parseDeviceURI extracts manufacturer and model from a CUPS device URI.
func parseDeviceURI(uri string) (manufacturer, model string) {
	// Parse URIs like usb://Canon/LBP2900?serial=XXX
	if strings.HasPrefix(uri, "usb://") {
		path := strings.TrimPrefix(uri, "usb://")
		// Remove query string.
		if idx := strings.Index(path, "?"); idx >= 0 {
			path = path[:idx]
		}
		parts := strings.SplitN(path, "/", 2)
		if len(parts) >= 1 {
			manufacturer = strings.ReplaceAll(parts[0], "%20", " ")
		}
		if len(parts) >= 2 {
			model = strings.ReplaceAll(parts[1], "%20", " ")
		}
	}
	return
}

// parseDescription extracts manufacturer and model from lpinfo quoted description fields.
func parseDescription(desc string) (manufacturer, model string) {
	desc = strings.TrimSpace(desc)
	if len(desc) >= 2 && desc[0] == '"' {
		end := strings.Index(desc[1:], "\"")
		if end >= 0 {
			fullName := desc[1 : end+1]
			parts := strings.SplitN(fullName, " ", 2)
			if len(parts) >= 1 {
				manufacturer = parts[0]
			}
			if len(parts) >= 2 {
				model = parts[1]
			}
		}
	}
	return
}

// checkHasDriver returns true if CUPS already has a PPD matching the model string.
func checkHasDriver(ctx context.Context, modelStr string) bool {
	cmd := exec.CommandContext(ctx, "lpinfo", "-m")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	lowerModel := strings.ToLower(modelStr)
	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(strings.ToLower(line), lowerModel) {
			return true
		}
	}
	return false
}

// findBestPPD searches CUPS for the best matching PPD URI for a given model.
func findBestPPD(ctx context.Context, model string) string {
	cmd := exec.CommandContext(ctx, "lpinfo", "-m")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lowerModel := strings.ToLower(model)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), lowerModel) {
			// Extract the PPD URI (first space-separated field).
			parts := strings.SplitN(line, " ", 2)
			if len(parts) >= 1 {
				return parts[0]
			}
		}
	}
	return ""
}

// sanitizePrinterName converts a model string into a valid CUPS printer name.
func sanitizePrinterName(model string) string {
	name := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, model)
	// Remove consecutive underscores.
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	return strings.Trim(name, "_")
}
