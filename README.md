# dji-flight 🚁

A Go CLI + web dashboard for parsing DJI drone SRT telemetry files into interactive flight visualizations.

**Zero regex. Zero external Go dependencies.** The parser uses pure string operations to handle DJI's `[key: value]` bracket format across all drone models.

## Features

- **SRT Parser** — Parses per-frame telemetry from DJI drones (Neo 2, Mavic, Mini, Air, Phantom, and more)
- **Flight Statistics** — Duration, distance, max/avg speed, altitude gain/loss, hover time, surveyed area
- **Solar Position** — Computes sun elevation angle at takeoff (golden hour detection)
- **EIS Analysis** — Quaternion-based gimbal tracking error, digital shift magnitude, over-border events
- **Camera Analysis** — Average color temperature, exposure value, high-ISO % (noise risk)
- **Reverse Geocoding** — Identifies where you flew via OpenStreetMap (cached, no API key)
- **Wind Estimation** — Estimates wind direction and speed from ground-speed asymmetry
- **Gimbal Stability** — Pitch/roll variance as a proxy for turbulence
- **Export** — CSV, JSON, GPX (Google Earth / Strava), GeoJSON
- **Web Dashboard** — Interactive map, 4-tab Chart.js charts, playback animation, aggregate header stats
- **CLI** — `info`, `export`, `serve`, `scan`, `copy`, `stop` commands
- **Single binary** — Web UI embedded via `go:embed`; no separate frontend build

---

## Install

### One-liner (macOS / Linux)

```bash
curl -sSL https://raw.githubusercontent.com/bhanurp/dji-flight/main/install.sh | bash
```

### Download binary

Grab the latest release from [github.com/bhanurp/dji-flight/releases](https://github.com/bhanurp/dji-flight/releases):

```bash
# macOS Apple Silicon
curl -Lo dji-flight https://github.com/bhanurp/dji-flight/releases/latest/download/dji-flight-darwin-arm64
chmod +x dji-flight && sudo mv dji-flight /usr/local/bin/

# macOS Intel
curl -Lo dji-flight https://github.com/bhanurp/dji-flight/releases/latest/download/dji-flight-darwin-amd64
chmod +x dji-flight && sudo mv dji-flight /usr/local/bin/

# Linux amd64
curl -Lo dji-flight https://github.com/bhanurp/dji-flight/releases/latest/download/dji-flight-linux-amd64
chmod +x dji-flight && sudo mv dji-flight /usr/local/bin/
```

### Build from source

```bash
git clone https://github.com/bhanurp/dji-flight
cd dji-flight
make build          # builds ./dji-flight for your current platform
make install-local  # installs to /usr/local/bin/dji-flight
```

### Cross-compile releases

```bash
make release   # builds 5 binaries in dist/
# dist/dji-flight-darwin-amd64
# dist/dji-flight-darwin-arm64
# dist/dji-flight-linux-amd64
# dist/dji-flight-linux-arm64
# dist/dji-flight-windows-amd64.exe
```

---

## CLI Usage

### Show flight summary

```bash
dji-flight info DJI_0001.SRT
```

Prints duration, distance, GPS coordinates, altitude, speed, wind estimate, and gimbal stability.

### Launch web dashboard

```bash
# Single file or multiple files
dji-flight serve DJI_0001.SRT DJI_0002.SRT

# Entire directory — scans recursively for all SRT files
dji-flight serve /Volumes/DJI_NEO2/DCIM

# Custom port
dji-flight serve --port 3000
DJI_FLIGHT_PORT=3000 dji-flight serve /path/to/flights

# No pre-loaded flights (upload-only mode)
dji-flight serve
```

If the port is already in use, you'll be prompted:
```
Port 8080 is in use. Kill it and reuse? [Y/n/new-port]:
```

### Scan a directory

```bash
dji-flight scan /Volumes/DJI_NEO2/DCIM         # print summaries of all SRT files
dji-flight scan /Volumes/DJI_NEO2/DCIM --json  # output JSON
```

### Copy files from drone storage

Copies SRT (and optionally video) files from a mounted drone volume, organized into date folders:

```bash
dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights
# → ./my-flights/2024-01-15/DJI_0001.SRT
# → ./my-flights/2024-01-15/DJI_0001.MP4

dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights --srt-only   # skip video
dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights --dry-run    # preview only
```

### Export to file

```bash
dji-flight export DJI_0001.SRT -o flight.gpx       # GPX (Google Earth, Strava)
dji-flight export DJI_0001.SRT -o flight.csv        # CSV (frame-level telemetry)
dji-flight export DJI_0001.SRT -o flight.geojson    # GeoJSON
dji-flight export DJI_0001.SRT -o flight.json       # Full JSON dump
```

### Stop all running instances

```bash
dji-flight stop
```

---

## Web Dashboard

Launch with `dji-flight serve` and open `http://localhost:8080`. The dashboard also works as a **standalone HTML file** — open it directly in a browser and drop SRT files onto it (no server required).

### Header
Live aggregate stats across all loaded flights: **Flights · Total Time · Total Dist · Peak Alt · Peak Speed · Location**

### Map
- Speed-colored flight path (teal = slow → blue-purple = fast)
- **Pulsing orange marker** at max altitude point
- **Pulsing blue marker** at max speed point
- Start / Landing / Home markers with coordinate popups
- **Satellite layer toggle** (Esri World Imagery)
- Chart hover syncs a cursor marker on the map in real time

### Charts
Four interactive tabs powered by Chart.js:
- **Altitude (m)** — relative altitude profile with gradient fill
- **Speed (km/h)** — horizontal speed profile
- **Gimbal** — pitch and roll over time
- **EIS / Shake** — gimbal quaternion tracking error and digital shift magnitude

### Flight animation
Click **▶ Play** to replay the drone's path with a live HUD showing altitude, speed, vertical speed and progress.

### Stats bar (16 metrics)
Duration · Distance · Max Altitude · Max Speed · Avg Speed · Alt Gain · Alt Loss · Hover Time · Area · Sun Angle · EIS Strain · Wind · Stability · Color Temp · Date · Location

### Sidebar
- **Scrollable section**: mini SVG path thumbnail, metadata card, dropzone, flight list with `↑↓` keyboard navigation
- **Pinned section** (always visible at bottom):
  - Wind compass with direction and speed + gimbal stability bar
  - Flight Analysis card: solar elevation, hover time %, surveyed area, EIS strain, color temperature, high-ISO %, average EV

---

## Use as a Go library

```go
import (
    "fmt"
    "github.com/bhanurp/dji-flight/pkg/parser"
    "github.com/bhanurp/dji-flight/pkg/export"
)

fd, err := parser.ParseFile("DJI_0001.SRT")
if err != nil {
    panic(err)
}

fmt.Printf("Date:         %s\n",   fd.FlightDate)
fmt.Printf("Duration:     %.0fs\n", fd.DurationSeconds)
fmt.Printf("Distance:     %.0fm\n", fd.TotalDistanceM)
fmt.Printf("Max Alt:      %.1fm\n", fd.MaxAltitudeM)
fmt.Printf("Alt Gain:     %.1fm\n", fd.AltitudeGainM)
fmt.Printf("Hover Time:   %.0fs\n", fd.HoverTimeSeconds)
fmt.Printf("Area:         %.0fm²\n", fd.SurveyedAreaM2)
fmt.Printf("Sun Angle:    %.1f°\n", fd.SolarElevationDeg)
fmt.Printf("Color Temp:   %.0fK\n", fd.AvgColorTempK)
fmt.Printf("High-ISO %%:   %.1f%%\n", fd.HighISOPct)
fmt.Printf("EIS Strain:   %.2f° avg\n", fd.EISAnalysis.AvgStrainDeg)
fmt.Printf("Max Speed:    %.1f km/h\n", fd.MaxSpeedMs*3.6)
fmt.Printf("Wind:         %.1f km/h @ %.0f°\n", fd.EstimatedWindSpeed*3.6, fd.EstimatedWindDir)

for _, frame := range fd.Frames {
    fmt.Printf("  [%d] lat=%.6f lon=%.6f alt=%.1fm spd=%.1fm/s eis=%.2f°\n",
        frame.Index, frame.Latitude, frame.Longitude,
        frame.RelAltitude, frame.HSpeed, frame.EISStrain)
}

export.ToGPX(fd, "flight.gpx")
export.ToCSV(fd, "flight.csv")
export.ToGeoJSON(fd, "flight.geojson")
```

### FlightData fields

| Field | Type | Description |
|---|---|---|
| `FileName` | string | Source SRT filename |
| `Frames` | []Frame | Per-frame telemetry |
| `FlightDate` | string | `"2024-01-15"` |
| `DurationSeconds` | float64 | Total flight time |
| `TotalDistanceM` | float64 | Distance flown (Haversine) |
| `MaxAltitudeM` | float64 | Peak relative altitude |
| `MinRelAltitudeM` | float64 | Lowest relative altitude |
| `AltitudeGainM` | float64 | Cumulative meters climbed |
| `AltitudeLossM` | float64 | Cumulative meters descended |
| `MaxSpeedMs` | float64 | Peak speed (m/s) |
| `AvgSpeedMs` | float64 | Average speed (m/s) |
| `MaxAltitudeFrameIdx` | int | Frame index at peak altitude |
| `MaxSpeedFrameIdx` | int | Frame index at peak speed |
| `HoverTimeSeconds` | float64 | Seconds with HSpeed < 0.3 m/s |
| `SurveyedAreaM2` | float64 | Convex hull of GPS track (m²) |
| `SolarElevationDeg` | float64 | Sun angle at takeoff (degrees) |
| `GoldenHour` | bool | Sun within 6° of horizon |
| `AvgColorTempK` | float64 | Mean color temperature (Kelvin) |
| `AvgEV` | float64 | Mean exposure value |
| `HighISOPct` | float64 | % of frames with ISO > 800 |
| `HighISOFrames` | int | Count of high-ISO frames |
| `EISAnalysis` | EISAnalysis | Gimbal tracking error summary |
| `StartCoords` | [2]float64 | `[lat, lon]` at takeoff |
| `EndCoords` | [2]float64 | `[lat, lon]` at landing |
| `EstimatedWindDir` | float64 | Wind direction (degrees) |
| `EstimatedWindSpeed` | float64 | Estimated wind speed (m/s) |
| `GimbalStability` | GimbalStats | Pitch/roll variance and deltas |

### Frame fields (per-frame telemetry)

| Field | Type | Description |
|---|---|---|
| `Latitude`, `Longitude` | float64 | GPS coordinates |
| `RelAltitude` | float64 | Altitude above takeoff (m) |
| `AbsAltitude` | float64 | Altitude above sea level (m) |
| `HSpeed` | float64 | Horizontal speed (m/s, computed) |
| `VSpeed` | float64 | Vertical speed (m/s, computed) |
| `Bearing` | float64 | Heading in degrees (computed) |
| `ISO` | int | Camera ISO |
| `Shutter` | string | Shutter speed e.g. `"1/1000"` |
| `FNum` | float64 | Aperture (f-number) |
| `CT` | int | Color temperature (Kelvin) |
| `EV` | float64 | Exposure value |
| `GimbalPitch/Roll/Yaw` | float64 | Gimbal orientation (degrees) |
| `EISStrain` | float64 | Quaternion tracking error (degrees) |
| `EISShiftX/Y/Mag` | float64 | Digital EIS crop shift |

---

## How the parser works

DJI SRT files embed telemetry in subtitle files:

```
1
00:00:00,000 --> 00:00:00,016
<font size="28">FrameCnt: 1, DiffTime: 16ms
2026-04-11 07:45:24.521
[iso: 100] [shutter: 1/1000.0] [fnum: 2.2] [ev: 0] [ct: 5134]
[latitude: 15.262774] [longitude: 77.711158] [rel_alt: 7.300 abs_alt: 509.222]
[shift x: 0.00, y: 0.00] [pp_target: 0.485, 0.000, 0.000, -0.875]</font>
```

Algorithm:
1. **Split into blocks** — blank lines separate subtitle entries
2. **Strip HTML** — skip anything between `<` and `>`
3. **Parse brackets** — split on `[`, isolate key-value content, split on first `:`
4. **Multi-pair brackets** — `[rel_alt: 7.300 abs_alt: 509.222]` recurses on remainder
5. **Type-map** — `switch` maps known keys to typed struct fields
6. **Compute derived** — Haversine distances, GPS-interval speeds (back-filled), altitude deltas, quaternion EIS strain, convex hull area, solar position (NOAA algorithm), wind bucket analysis

### Format variants handled

| Variant | Example | Drones |
|---|---|---|
| Colon-separated | `[latitude: 15.26]` | Neo 2, Mini 4 Pro |
| Multi-pair bracket | `[rel_alt: 7.3 abs_alt: 509.2]` | Neo 2, Air 3 |
| Space-separated | `[rel_alt 57.2 abs_alt 204.6]` | Older models |
| Typo correction | `[longtitude: ...]` | Various firmware |

---

## Architecture

```
dji-flight/
├── cmd/dji-flight/
│   └── main.go              CLI: info, export, serve, scan, copy, stop
├── pkg/
│   ├── parser/
│   │   ├── frame.go         FlightData, Frame, GimbalStats, EISAnalysis types
│   │   └── srt.go           SRT parser + all derived field computation
│   ├── export/
│   │   └── export.go        CSV, JSON, GPX, GeoJSON exporters
│   └── server/
│       ├── server.go        HTTP server + /api/geocode proxy
│       └── web/
│           └── index.html   Embedded dashboard (Leaflet + Chart.js)
├── .github/workflows/
│   └── release.yml          Cross-platform release automation
├── Makefile                 Build, release, install targets
├── install.sh               One-liner installer
├── go.mod
└── README.md
```

---

## License

MIT
