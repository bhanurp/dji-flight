package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bhanureddy/dji-flight/pkg/export"
	"github.com/bhanureddy/dji-flight/pkg/parser"
	"github.com/bhanureddy/dji-flight/pkg/server"
)

const version = "0.1.0"

const usage = `dji-flight — DJI drone SRT telemetry toolkit

Usage:
  dji-flight info    <file.SRT>              Show flight summary
  dji-flight export  <file.SRT> -o out.csv   Export to csv|json|gpx|geojson
  dji-flight serve   [file.SRT|dir ...]       Launch web viewer (default :8080)
  dji-flight scan    <directory>             Find and summarize all SRT files
  dji-flight copy    <source> <dest>         Copy SRT/video files by date
  dji-flight stop                            Stop all running dji-flight instances
  dji-flight version                         Print version

Examples:
  dji-flight info DJI_0001.SRT
  dji-flight export DJI_0001.SRT -o flight.gpx
  dji-flight serve DJI_0001.SRT DJI_0002.SRT
  dji-flight serve ./my-flights/
  dji-flight serve --port 3000
  dji-flight scan /Volumes/DJI_NEO2/DCIM
  dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights
  dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights --srt-only --dry-run
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(0)
	}

	cmd := os.Args[1]

	switch cmd {
	case "info":
		cmdInfo()
	case "export":
		cmdExport()
	case "serve":
		cmdServe()
	case "scan":
		cmdScan()
	case "copy":
		cmdCopy()
	case "stop":
		cmdStop()
	case "version":
		fmt.Printf("dji-flight v%s\n", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		// If the arg is a directory, treat it as "scan"
		// If it's a file, treat it as "info"
		if info, err := os.Stat(cmd); err == nil {
			if info.IsDir() {
				os.Args = append([]string{os.Args[0], "scan"}, os.Args[1:]...)
				cmdScan()
			} else {
				os.Args = append([]string{os.Args[0], "info"}, os.Args[1:]...)
				cmdInfo()
			}
		} else {
			fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
			fmt.Print(usage)
			os.Exit(1)
		}
	}
}

// ── info ────────────────────────────────────────────────────────

func cmdInfo() {
	if len(os.Args) < 3 {
		fatal("Usage: dji-flight info <file.SRT>")
	}

	fd, err := parser.ParseFile(os.Args[2])
	if err != nil {
		fatal("Parse error: %v", err)
	}

	printSummary(fd)
}

// ── export ──────────────────────────────────────────────────────

func cmdExport() {
	if len(os.Args) < 3 {
		fatal("Usage: dji-flight export <file.SRT> -o <output.csv|json|gpx|geojson>")
	}

	srtPath := os.Args[2]
	outPath := ""

	for i := 3; i < len(os.Args); i++ {
		if os.Args[i] == "-o" && i+1 < len(os.Args) {
			outPath = os.Args[i+1]
			i++
		}
	}

	if outPath == "" {
		// Default: same name with .json extension
		outPath = strings.TrimSuffix(srtPath, filepath.Ext(srtPath)) + ".json"
	}

	fd, err := parser.ParseFile(srtPath)
	if err != nil {
		fatal("Parse error: %v", err)
	}

	ext := strings.ToLower(filepath.Ext(outPath))
	switch ext {
	case ".csv":
		err = export.ToCSV(fd, outPath)
	case ".json":
		err = export.ToJSON(fd, outPath)
	case ".gpx":
		err = export.ToGPX(fd, outPath)
	case ".geojson":
		err = export.ToGeoJSON(fd, outPath)
	default:
		fatal("Unsupported format: %s (use .csv, .json, .gpx, or .geojson)", ext)
	}

	if err != nil {
		fatal("Export error: %v", err)
	}

	fmt.Printf("✔  Exported to %s\n", outPath)
}

// ── serve ───────────────────────────────────────────────────────

func cmdServe() {
	// Port priority: --port flag > DJI_FLIGHT_PORT env var > default 8080
	port := 8080
	if envPort := os.Getenv("DJI_FLIGHT_PORT"); envPort != "" {
		var p int
		if n, _ := fmt.Sscanf(envPort, "%d", &p); n == 1 && p > 0 {
			port = p
		}
	}
	var srtFiles []string

	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--port" && i+1 < len(os.Args) {
			fmt.Sscanf(os.Args[i+1], "%d", &port)
			i++
			continue
		}
		arg := os.Args[i]
		info, err := os.Stat(arg)
		if err != nil {
			continue
		}
		if info.IsDir() {
			// Walk directory for SRT files
			filepath.Walk(arg, func(p string, fi os.FileInfo, err error) error {
				if err == nil && !fi.IsDir() && isSRTFile(p) {
					srtFiles = append(srtFiles, p)
				}
				return nil
			})
		} else {
			srtFiles = append(srtFiles, arg)
		}
	}

	if err := server.Serve(port, srtFiles); err != nil {
		var portErr *server.PortInUseError
		if errors.As(err, &portErr) {
			fmt.Printf("\nPort %d is already in use (another dji-flight instance?).\n", portErr.Port)
			fmt.Print("Kill it and restart on the same port? [Y/n/new-port]: ")

			scanner := bufio.NewScanner(os.Stdin)
			scanner.Scan()
			answer := strings.TrimSpace(scanner.Text())

			switch {
			case answer == "" || strings.EqualFold(answer, "y"):
				killPort(portErr.Port)
				if err := server.Serve(portErr.Port, srtFiles); err != nil {
					fatal("Server error: %v", err)
				}
			case strings.EqualFold(answer, "n"):
				fmt.Println("Aborted.")
				os.Exit(0)
			default:
				// Treat as a port number
				newPort := 0
				fmt.Sscanf(answer, "%d", &newPort)
				if newPort == 0 {
					fatal("Invalid port: %s", answer)
				}
				if err := server.Serve(newPort, srtFiles); err != nil {
					fatal("Server error: %v", err)
				}
			}
			return
		}
		fatal("Server error: %v", err)
	}
}

// ── scan ────────────────────────────────────────────────────────

func cmdScan() {
	if len(os.Args) < 3 {
		fatal("Usage: dji-flight scan <directory>")
	}

	dir := os.Args[2]
	var srtFiles []string

	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if isSRTFile(p) {
			srtFiles = append(srtFiles, p)
		}
		return nil
	})

	if len(srtFiles) == 0 {
		fmt.Println("No SRT files found in", dir)
		return
	}

	fmt.Printf("Found %d SRT file(s)\n\n", len(srtFiles))

	for _, p := range srtFiles {
		fd, err := parser.ParseFile(p)
		if err != nil {
			fmt.Printf("⚠  %s: %v\n", filepath.Base(p), err)
			continue
		}
		printSummary(fd)
		fmt.Println()
	}

	// Also dump as JSON if --json flag
	for _, arg := range os.Args {
		if arg == "--json" {
			var all []*parser.FlightData
			for _, p := range srtFiles {
				fd, _ := parser.ParseFile(p)
				if fd != nil {
					all = append(all, fd)
				}
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(all)
			break
		}
	}
}

// ── Pretty print ────────────────────────────────────────────────

func printSummary(fd *parser.FlightData) {
	c := "\033[0;36m"
	g := "\033[0;32m"
	y := "\033[1;33m"
	d := "\033[2m"
	b := "\033[1m"
	r := "\033[0m"

	compassDirs := []string{"N", "NE", "E", "SE", "S", "SW", "W", "NW"}
	windDir := ""
	if fd.EstimatedWindSpeed > 0 {
		idx := int(fd.EstimatedWindDir/45.0) % 8
		windDir = compassDirs[idx]
	}

	durM := int(fd.DurationSeconds) / 60
	durS := int(fd.DurationSeconds) % 60

	fmt.Printf(`
%s╔══════════════════════════════════════════════════════╗%s
%s║  🚁 %s%-46s%s %s║%s
%s╚══════════════════════════════════════════════════════╝%s

  %sDuration%s      : %s%dm %ds%s  (%d frames)
  %sDistance%s      : %s%.0f m%s  (%.2f km)

%s─── GPS ──────────────────────────────────────────────%s
  Start         : %s%.6f, %.6f%s
  End           : %s%.6f, %.6f%s
  📍 Start Map  : %shttps://maps.google.com/?q=%.6f,%.6f%s
  📍 End Map    : %shttps://maps.google.com/?q=%.6f,%.6f%s

%s─── Altitude ─────────────────────────────────────────%s
  Max Altitude  : %s%.1f m%s

%s─── Speed ────────────────────────────────────────────%s
  Max Speed     : %s%.1f m/s%s  (%.1f km/h)
  Avg Speed     : %s%.1f m/s%s  (%.1f km/h)

%s─── Gimbal Stability ─────────────────────────────────%s
  Pitch Variance: %s%.4f%s  %s(lower = smoother)%s
  Roll Variance : %s%.4f%s
  Max Pitch Δ   : %s%.2f°%s  per frame
  Max Roll Δ    : %s%.2f°%s  per frame
`,
		c, r, c, b, fd.FileName, r, c, r, c, r,
		b, r, g, durM, durS, r, fd.FrameCount,
		b, r, g, fd.TotalDistanceM, r, fd.TotalDistanceM/1000,

		d, r,
		c, fd.StartCoords[0], fd.StartCoords[1], r,
		c, fd.EndCoords[0], fd.EndCoords[1], r,
		d, fd.StartCoords[0], fd.StartCoords[1], r,
		d, fd.EndCoords[0], fd.EndCoords[1], r,

		d, r,
		g, fd.MaxAltitudeM, r,

		d, r,
		g, fd.MaxSpeedMs, r, fd.MaxSpeedMs*3.6,
		g, fd.AvgSpeedMs, r, fd.AvgSpeedMs*3.6,

		d, r,
		y, fd.GimbalStability.PitchVariance, r, d, r,
		y, fd.GimbalStability.RollVariance, r,
		y, fd.GimbalStability.MaxPitchDelta, r,
		y, fd.GimbalStability.MaxRollDelta, r,
	)

	if fd.EstimatedWindSpeed > 0 {
		fmt.Printf(`
%s─── Wind Estimate ────────────────────────────────────%s
  Direction     : %s%s (%.0f°)%s
  Est. Speed    : %s%.1f m/s%s  (%.1f km/h)
  %s(based on ground speed asymmetry across headings)%s
`,
			d, r,
			y, windDir, fd.EstimatedWindDir, r,
			y, fd.EstimatedWindSpeed, r, fd.EstimatedWindSpeed*3.6,
			d, r,
		)
	}
}

// ── copy ─────────────────────────────────────────────────────────

func cmdCopy() {
	if len(os.Args) < 4 {
		fatal("Usage: dji-flight copy <source> <dest> [--srt-only] [--dry-run]")
	}

	src := os.Args[2]
	dst := os.Args[3]

	srtOnly := false
	dryRun := false
	for _, arg := range os.Args[4:] {
		switch arg {
		case "--srt-only":
			srtOnly = true
		case "--dry-run":
			dryRun = true
		}
	}

	videoExts := map[string]bool{".mp4": true, ".mov": true, ".avi": true}

	type copyPair struct{ src, dst string }
	var pairs []copyPair

	filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		isSRT := ext == ".srt"
		isVideo := videoExts[ext]
		if !isSRT && (!isVideo || srtOnly) {
			return nil
		}

		// Determine date from SRT parse or fall back to file mtime
		date := ""
		if isSRT {
			if fd, err := parser.ParseFile(p); err == nil && fd.FlightDate != "" {
				date = fd.FlightDate
			}
		}
		if date == "" {
			date = info.ModTime().Format("2006-01-02")
		}

		pairs = append(pairs, copyPair{
			src: p,
			dst: filepath.Join(dst, date, filepath.Base(p)),
		})
		return nil
	})

	if len(pairs) == 0 {
		fmt.Println("No files found to copy.")
		return
	}

	for _, pair := range pairs {
		if dryRun {
			fmt.Printf("[dry-run] %s → %s\n", pair.src, pair.dst)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(pair.dst), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "⚠  mkdir: %v\n", err)
			continue
		}
		if err := copyFile(pair.src, pair.dst); err != nil {
			fmt.Fprintf(os.Stderr, "⚠  %s: %v\n", pair.src, err)
		} else {
			fmt.Printf("✔  %s\n", pair.dst)
		}
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// ── Helpers ─────────────────────────────────────────────────────

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// isSRTFile returns true for .srt files, excluding macOS metadata (._) files.
func isSRTFile(path string) bool {
	base := filepath.Base(path)
	return strings.ToLower(filepath.Ext(path)) == ".srt" && !strings.HasPrefix(base, "._")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ── stop ─────────────────────────────────────────────────────────

func cmdStop() {
	out, err := exec.Command("pgrep", "-f", "dji-flight serve").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		fmt.Println("No running dji-flight instances found.")
		return
	}
	killed := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid := strings.TrimSpace(line)
		if pid == "" {
			continue
		}
		if err := exec.Command("kill", pid).Run(); err == nil {
			fmt.Printf("Stopped PID %s\n", pid)
			killed++
		}
	}
	if killed == 0 {
		fmt.Println("No instances stopped.")
	} else {
		fmt.Printf("Stopped %d instance(s).\n", killed)
	}
}

// killPort uses lsof to find and SIGTERM the process listening on the port.
func killPort(port int) {
	out, err := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port)).Output()
	if err != nil || len(out) == 0 {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid := strings.TrimSpace(line)
		if pid == "" {
			continue
		}
		exec.Command("kill", pid).Run()
		fmt.Printf("Stopped PID %s\n", pid)
	}
	// Brief pause for the OS to release the port
	exec.Command("sleep", "0.5").Run()
}
