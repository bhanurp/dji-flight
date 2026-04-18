package server

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bhanureddy/dji-flight/pkg/parser"
)

// geocodeCache avoids hammering Nominatim for the same coordinates.
// Key: "lat,lon" rounded to 3 decimal places (~111m precision).
var (
	geocodeMu    sync.Mutex
	geocodeCache = map[string]string{}
)

//go:embed all:web
var webFS embed.FS

// Serve starts the web server on the given port, pre-loaded with
// telemetry data from the provided SRT files (can be empty for
// upload-only mode).
func Serve(port int, srtPaths []string) error {
	mux := http.NewServeMux()

	// Pre-parse any SRT files passed on the command line
	var flights []*parser.FlightData
	for _, p := range srtPaths {
		fd, err := parser.ParseFile(p)
		if err != nil {
			log.Printf("warning: could not parse %s: %v", p, err)
			continue
		}
		flights = append(flights, fd)
	}

	// API: return pre-loaded flight data
	mux.HandleFunc("/api/flights", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(flights)
	})

	// API: upload an SRT file and get parsed data back
	mux.HandleFunc("/api/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		r.ParseMultipartForm(32 << 20) // 32MB max
		file, header, err := r.FormFile("srt")
		if err != nil {
			http.Error(w, "no file: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		fd, err := parser.Parse(file, header.Filename)
		if err != nil {
			http.Error(w, "parse error: "+err.Error(), http.StatusBadRequest)
			return
		}

		flights = append(flights, fd)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fd)
	})

	// API: parse an SRT file from a local path
	mux.HandleFunc("/api/parse", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "?path= required", http.StatusBadRequest)
			return
		}

		fd, err := parser.ParseFile(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		flights = append(flights, fd)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(fd)
	})

	// API: scan a directory for SRT files
	mux.HandleFunc("/api/scan", func(w http.ResponseWriter, r *http.Request) {
		dir := r.URL.Query().Get("dir")
		if dir == "" {
			http.Error(w, "?dir= required", http.StatusBadRequest)
			return
		}

		var found []string
		filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			base := filepath.Base(p)
			if strings.ToLower(filepath.Ext(p)) == ".srt" && !strings.HasPrefix(base, "._") {
				found = append(found, p)
			}
			return nil
		})

		// Parse all found SRT files
		var results []*parser.FlightData
		for _, p := range found {
			fd, err := parser.ParseFile(p)
			if err != nil {
				continue
			}
			results = append(results, fd)
			flights = append(flights, fd)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(results)
	})

	// API: reverse geocode lat/lon via Nominatim (proxied to avoid CORS)
	mux.HandleFunc("/api/geocode", func(w http.ResponseWriter, r *http.Request) {
		lat := r.URL.Query().Get("lat")
		lon := r.URL.Query().Get("lon")
		if lat == "" || lon == "" {
			http.Error(w, "lat and lon required", http.StatusBadRequest)
			return
		}

		latF, _ := strconv.ParseFloat(lat, 64)
		lonF, _ := strconv.ParseFloat(lon, 64)
		cacheKey := fmt.Sprintf("%.3f,%.3f", latF, lonF)

		geocodeMu.Lock()
		if cached, ok := geocodeCache[cacheKey]; ok {
			geocodeMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, cached)
			return
		}
		geocodeMu.Unlock()

		req, err := http.NewRequest("GET",
			fmt.Sprintf("https://nominatim.openstreetmap.org/reverse?lat=%s&lon=%s&format=json&zoom=10", lat, lon),
			nil)
		if err != nil {
			http.Error(w, "request error", http.StatusInternalServerError)
			return
		}
		req.Header.Set("User-Agent", "dji-flight/0.2 (github.com/bhanureddy/dji-flight)")
		req.Header.Set("Accept-Language", "en")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != 200 {
			http.Error(w, "geocode failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		var nominatim struct {
			DisplayName string `json:"display_name"`
			Address     struct {
				City        string `json:"city"`
				Town        string `json:"town"`
				Village     string `json:"village"`
				State       string `json:"state"`
				CountryCode string `json:"country_code"`
			} `json:"address"`
		}

		result := string(body)
		if err := json.Unmarshal(body, &nominatim); err == nil {
			city := nominatim.Address.City
			if city == "" {
				city = nominatim.Address.Town
			}
			if city == "" {
				city = nominatim.Address.Village
			}
			parts := []string{}
			if city != "" {
				parts = append(parts, city)
			}
			if nominatim.Address.State != "" {
				parts = append(parts, nominatim.Address.State)
			}
			if nominatim.Address.CountryCode != "" {
				parts = append(parts, strings.ToUpper(nominatim.Address.CountryCode))
			}
			label := strings.Join(parts, ", ")
			result = fmt.Sprintf(`{"label":%q,"display_name":%q}`, label, nominatim.DisplayName)
		}

		geocodeMu.Lock()
		geocodeCache[cacheKey] = result
		geocodeMu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, result)
	})

	// API: export flight data in different formats
	mux.HandleFunc("/api/export/", func(w http.ResponseWriter, r *http.Request) {
		// Path: /api/export/{index}/{format}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) < 4 {
			http.Error(w, "usage: /api/export/{index}/{csv|json|gpx|geojson}", http.StatusBadRequest)
			return
		}

		idx := 0
		fmt.Sscanf(parts[2], "%d", &idx)
		if idx < 0 || idx >= len(flights) {
			http.Error(w, "invalid flight index", http.StatusBadRequest)
			return
		}

		fd := flights[idx]
		format := parts[3]

		tmpFile := filepath.Join(os.TempDir(), "dji-flight-export."+format)
		defer os.Remove(tmpFile)

		var exportErr error
		switch format {
		case "csv":
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", "attachment; filename=flight.csv")
		case "json":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", "attachment; filename=flight.json")
		case "gpx":
			w.Header().Set("Content-Type", "application/gpx+xml")
			w.Header().Set("Content-Disposition", "attachment; filename=flight.gpx")
		case "geojson":
			w.Header().Set("Content-Type", "application/geo+json")
			w.Header().Set("Content-Disposition", "attachment; filename=flight.geojson")
		default:
			http.Error(w, "format must be csv, json, gpx, or geojson", http.StatusBadRequest)
			return
		}

		// Write to temp file then stream back
		switch format {
		case "json":
			exportErr = json.NewEncoder(w).Encode(fd)
		default:
			// For CSV/GPX/GeoJSON, write to temp then copy
			switch format {
			case "csv":
				exportErr = writeCSVDirect(w, fd)
			case "gpx":
				exportErr = writeGPXDirect(w, fd)
			case "geojson":
				exportErr = writeGeoJSONDirect(w, fd)
			}
		}
		if exportErr != nil {
			log.Printf("export error: %v", exportErr)
		}
	})

	// Serve the embedded web UI
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Try embedded FS first
		content, err := webFS.ReadFile("web" + path)
		if err != nil {
			// Fallback: serve index.html for SPA routing
			content, err = webFS.ReadFile("web/index.html")
			if err != nil {
				http.Error(w, "not found", 404)
				return
			}
		}

		// Set content type
		switch {
		case strings.HasSuffix(path, ".html"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case strings.HasSuffix(path, ".js"):
			w.Header().Set("Content-Type", "application/javascript")
		case strings.HasSuffix(path, ".css"):
			w.Header().Set("Content-Type", "text/css")
		}
		w.Write(content)
	})

	ln, err := bindPort(port)
	if err != nil {
		return &PortInUseError{Port: port}
	}
	actualPort := ln.Addr().(*net.TCPAddr).Port

	log.Printf("🚁 DJI Flight Viewer running at http://localhost:%d", actualPort)
	if len(flights) > 0 {
		log.Printf("   Loaded %d flight(s)", len(flights))
	}
	return http.Serve(ln, mux)
}

// ── Port helpers ─────────────────────────────────────────────────

// bindPort binds to the given port and returns the listener.
func bindPort(port int) (net.Listener, error) {
	return net.Listen("tcp", fmt.Sprintf(":%d", port))
}

// PortInUseError is returned when the requested port is already bound.
type PortInUseError struct{ Port int }

func (e *PortInUseError) Error() string {
	return fmt.Sprintf("port %d is already in use", e.Port)
}

// ── Direct-to-writer export helpers ─────────────────────────────

func writeCSVDirect(w io.Writer, fd *parser.FlightData) error {
	fmt.Fprintln(w, "index,timestamp,lat,lon,rel_alt,h_speed,v_speed,bearing,iso,shutter")
	for _, fr := range fd.Frames {
		fmt.Fprintf(w, "%d,%s,%.6f,%.6f,%.2f,%.2f,%.2f,%.1f,%d,%s\n",
			fr.Index,
			fr.Timestamp.Format("2006-01-02T15:04:05"),
			fr.Latitude, fr.Longitude, fr.RelAltitude,
			fr.HSpeed, fr.VSpeed, fr.Bearing,
			fr.ISO, fr.Shutter)
	}
	return nil
}

func writeGPXDirect(w io.Writer, fd *parser.FlightData) error {
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="dji-flight" xmlns="http://www.topografix.com/GPX/1/1">
<trk><n>%s</n><trkseg>
`, fd.FileName)
	for _, fr := range fd.Frames {
		if fr.Latitude == 0 && fr.Longitude == 0 {
			continue
		}
		fmt.Fprintf(w, `<trkpt lat="%f" lon="%f"><ele>%f</ele><time>%s</time></trkpt>
`,
			fr.Latitude, fr.Longitude, fr.AbsAltitude,
			fr.Timestamp.Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprint(w, "</trkseg></trk></gpx>\n")
	return nil
}

func writeGeoJSONDirect(w io.Writer, fd *parser.FlightData) error {
	coords := make([][]float64, 0, len(fd.Frames))
	for _, fr := range fd.Frames {
		if fr.Latitude == 0 && fr.Longitude == 0 {
			continue
		}
		coords = append(coords, []float64{fr.Longitude, fr.Latitude, fr.RelAltitude})
	}
	geojson := map[string]any{
		"type": "FeatureCollection",
		"features": []map[string]any{
			{
				"type":       "Feature",
				"properties": map[string]any{"name": fd.FileName},
				"geometry":   map[string]any{"type": "LineString", "coordinates": coords},
			},
		},
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(geojson)
}
