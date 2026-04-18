# dji-flight 🚁

A Go library and CLI for parsing DJI drone SRT telemetry files — with a built-in web dashboard for flight visualization.

**Zero regex.** The parser uses pure string operations (`strings.Split`, `strings.Index`) to parse DJI's `[key: value]` bracket format. **Zero external Go dependencies.**

## Features

- **SRT Parser** — Parses per-frame telemetry from DJI drones (Neo 2, Mavic, Mini, Air, Phantom, and more)
- **Flight Statistics** — Duration, distance (Haversine), max/avg speed, altitude gain/loss, min altitude, flight date
- **Reverse Geocoding** — Identifies where you flew (city, region, country) via OpenStreetMap
- **Wind Estimation** — Estimates wind direction and speed from ground-speed asymmetry across headings
- **Gimbal Stability Analysis** — Measures gimbal pitch/roll variance as a proxy for turbulence
- **Export** — CSV, JSON, GPX (Google Earth / Strava), GeoJSON
- **Web Dashboard** — Interactive map, Chart.js charts, drag-drop upload, flight animation playback
- **CLI** — `info`, `export`, `serve`, `scan`, `copy` commands
- **Single binary** — Web UI is embedded; no separate frontend build or runtime required

---

## Install

### One-liner (macOS / Linux)

```bash
curl -sSL https://raw.githubusercontent.com/bhanureddy/dji-flight/main/install.sh | bash
```

### Go install

```bash
go install github.com/bhanureddy/dji-flight/cmd/dji-flight@latest
```

### Build from source

```bash
git clone https://github.com/bhanureddy/dji-flight
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

Prints duration, distance, GPS coordinates with map links, altitude, speed, wind estimate, and gimbal stability.

### Launch web dashboard

```bash
dji-flight serve DJI_0001.SRT DJI_0002.SRT
# Open http://localhost:8080

dji-flight serve --port 3000   # custom port
dji-flight serve               # upload-only mode (no pre-loaded flights)
```

### Scan a directory

```bash
dji-flight scan /Volumes/DJI_NEO2/DCIM         # print summaries
dji-flight scan /Volumes/DJI_NEO2/DCIM --json  # output JSON
```

### Copy files from drone storage

Copies SRT (and optionally video) files from a mounted drone volume, organized into date folders:

```bash
dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights
# → ./my-flights/2024-01-15/DJI_0001.SRT
# → ./my-flights/2024-01-15/DJI_0001.MP4

dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights --srt-only   # skip video files
dji-flight copy /Volumes/DJI_NEO2/DCIM ./my-flights --dry-run    # preview without copying
```

### Export to file

```bash
dji-flight export DJI_0001.SRT -o flight.gpx       # GPX (Google Earth, Strava)
dji-flight export DJI_0001.SRT -o flight.csv        # CSV (frame-level telemetry)
dji-flight export DJI_0001.SRT -o flight.geojson    # GeoJSON
dji-flight export DJI_0001.SRT -o flight.json       # Full JSON dump
```

---

## Web Dashboard

Launch with `dji-flight serve` and open `http://localhost:8080`. The dashboard also works as a **standalone HTML file** — open it directly in a browser and drop SRT files onto it (no Go server required).

### Map
- Speed-colored flight path (teal = slow → blue-purple = fast)
- **Pulsing orange marker** at max altitude point
- **Pulsing blue marker** at max speed point
- Start / Landing / Home markers with coordinate popups
- **Satellite layer toggle** (Esri World Imagery)

### Charts
Three interactive tabs powered by Chart.js:
- **Altitude (m)** — relative altitude profile with gradient fill
- **Speed (km/h)** — horizontal speed profile
- **Gimbal** — pitch and roll over time

Hover over any chart to move a live cursor marker along the flight path on the map.

### Flight animation
Click **▶ Play** to replay the drone's path in real time with a 🚁 marker.

### Stats bar (12 metrics)
Duration · Distance · Max Altitude · Max Speed · Avg Speed · Alt Gain · Alt Loss · Frames · Wind · Stability · Date · Location

### Sidebar
- Mini SVG map thumbnail of the flight path
- Metadata card: date, reverse-geocoded location, altitude gain/loss
- Wind compass with estimated direction and speed
- Gimbal stability bar
- Export buttons per flight: CSV, GPX, GeoJSON

---

## Use as a Go library

```go
import (
    "fmt"
    "github.com/bhanureddy/dji-flight/pkg/parser"
    "github.com/bhanureddy/dji-flight/pkg/export"
)

fd, err := parser.ParseFile("DJI_0001.SRT")
if err != nil {
    panic(err)
}

fmt.Printf("Date:       %s\n",   fd.FlightDate)
fmt.Printf("Duration:   %.0fs\n", fd.DurationSeconds)
fmt.Printf("Distance:   %.0fm\n", fd.TotalDistanceM)
fmt.Printf("Max Alt:    %.1fm\n", fd.MaxAltitudeM)
fmt.Printf("Alt Gain:   %.1fm\n", fd.AltitudeGainM)
fmt.Printf("Alt Loss:   %.1fm\n", fd.AltitudeLossM)
fmt.Printf("Max Speed:  %.1f km/h\n", fd.MaxSpeedMs*3.6)
fmt.Printf("Wind:       %.1f km/h @ %.0f°\n", fd.EstimatedWindSpeed*3.6, fd.EstimatedWindDir)

// Access individual frames
for _, frame := range fd.Frames {
    fmt.Printf("  [%d] lat=%.6f lon=%.6f alt=%.1fm speed=%.1fm/s\n",
        frame.Index, frame.Latitude, frame.Longitude,
        frame.RelAltitude, frame.HSpeed)
}

// Export
export.ToGPX(fd, "flight.gpx")
export.ToCSV(fd, "flight.csv")
export.ToGeoJSON(fd, "flight.geojson")
```

### FlightData fields

| Field | Type | Description |
|---|---|---|
| `FileName` | string | Source SRT filename |
| `Frames` | []Frame | Per-frame telemetry |
| `FlightDate` | string | `"2024-01-15"` (from first timestamp) |
| `DurationSeconds` | float64 | Total flight time |
| `TotalDistanceM` | float64 | Distance flown (Haversine sum) |
| `MaxAltitudeM` | float64 | Peak relative altitude |
| `MinRelAltitudeM` | float64 | Lowest relative altitude |
| `AltitudeGainM` | float64 | Cumulative meters climbed |
| `AltitudeLossM` | float64 | Cumulative meters descended |
| `MaxSpeedMs` | float64 | Peak speed in m/s |
| `AvgSpeedMs` | float64 | Average speed in m/s |
| `MaxAltitudeFrameIdx` | int | Index into Frames[] at peak altitude |
| `MaxSpeedFrameIdx` | int | Index into Frames[] at peak speed |
| `StartCoords` | [2]float64 | `[lat, lon]` at takeoff |
| `EndCoords` | [2]float64 | `[lat, lon]` at landing |
| `EstimatedWindDir` | float64 | Wind direction in degrees |
| `EstimatedWindSpeed` | float64 | Estimated wind speed in m/s |
| `GimbalStability` | GimbalStats | Pitch/roll variance and deltas |

---

## How the parser works (no regex)

DJI SRT files use a bracket-based telemetry format embedded in subtitle files:

```
1
00:00:00,000 --> 00:00:00,033
<font size="28">FrameCnt: 1, DiffTime: 33ms
2026-01-30 09:58:21.637
[iso: 100] [shutter: 1/1250.0] [fnum: 2.2]
[latitude: -29.685883] [longitude: -53.777843]
[rel_alt: 57.200 abs_alt: 204.644]</font>
```

The parsing algorithm:
1. **Split into blocks** — blank lines separate subtitle entries
2. **Strip HTML** — walk chars, skip anything between `<` and `>`
3. **Parse brackets** — split on `[`, then for each piece split on `]` to isolate key-value content, then split on `:` for the pair
4. **Type-map** — a `switch` maps known keys (`latitude`, `iso`, `gb_pitch`, etc.) to typed struct fields
5. **Compute derived** — Haversine for distances, deltas for speeds and altitude changes, variance for gimbal stability, 8-bucket wind analysis

### Format variants handled

| Variant | Example | Drones |
|---|---|---|
| Colon-separated | `[latitude: -29.68]` | Neo 2, Mini 4 |
| Extra spaces | `[latitude : -29.68]` | Mavic 2 |
| Space-separated pairs | `[rel_alt 57.2 abs_alt 204.6]` | Older models |
| Typo correction | `[longtitude: ...]` → `longitude` | Various |

---

## Architecture

```
dji-flight/
├── cmd/dji-flight/
│   └── main.go              CLI: info, export, serve, scan, copy
├── pkg/
│   ├── parser/
│   │   ├── frame.go         Data types (Frame, FlightData, GimbalStats)
│   │   └── srt.go           SRT parser + derived field computation
│   ├── export/
│   │   └── export.go        CSV, JSON, GPX, GeoJSON exporters
│   └── server/
│       ├── server.go        HTTP server + /api/geocode proxy
│       └── web/
│           └── index.html   Embedded dashboard (Leaflet + Chart.js)
├── Makefile                 Build, release, install targets
├── install.sh               One-liner installer
├── go.mod
└── README.md
```

---

## Wind estimation

The wind estimator buckets frames into 8 compass directions based on their heading, then computes average ground speed per direction. The direction with the slowest average speed is the likely headwind direction. Wind speed = (fastest_dir_avg − slowest_dir_avg) / 2.

Requires the drone to have flown in at least 3 different directions.

## Gimbal stability

The stability score is derived from the variance of `gb_pitch` and `gb_roll` across all frames. High variance means the gimbal was constantly compensating — indicating wind, turbulence, or aggressive maneuvers. Score range: 0 (chaotic) to 100 (perfectly smooth).

---

## License

MIT
