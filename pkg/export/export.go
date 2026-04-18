package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/bhanureddy/dji-flight/pkg/parser"
)

// ToCSV writes frame-level telemetry as a CSV file.
func ToCSV(fd *parser.FlightData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"index", "timestamp", "latitude", "longitude",
		"rel_alt", "abs_alt", "h_speed_ms", "v_speed_ms",
		"bearing", "distance_from_home",
		"iso", "shutter", "fnum", "ev",
		"gb_yaw", "gb_pitch", "gb_roll",
	}
	w.Write(header)

	for _, fr := range fd.Frames {
		row := []string{
			strconv.Itoa(fr.Index),
			fr.Timestamp.Format("2006-01-02 15:04:05.000"),
			ff(fr.Latitude, 6), ff(fr.Longitude, 6),
			ff(fr.RelAltitude, 2), ff(fr.AbsAltitude, 2),
			ff(fr.HSpeed, 2), ff(fr.VSpeed, 2),
			ff(fr.Bearing, 1), ff(fr.Distance, 1),
			strconv.Itoa(fr.ISO), fr.Shutter,
			ff(fr.FNum, 1), ff(fr.EV, 1),
			ff(fr.GimbalYaw, 1), ff(fr.GimbalPitch, 1), ff(fr.GimbalRoll, 1),
		}
		w.Write(row)
	}
	return nil
}

// ToJSON writes the full FlightData as pretty-printed JSON.
func ToJSON(fd *parser.FlightData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(fd)
}

// ToGPX writes GPS track as a GPX file compatible with Google Earth,
// Strava, and other mapping tools.
func ToGPX(fd *parser.FlightData, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, `<?xml version="1.0" encoding="UTF-8"?>
<gpx version="1.1" creator="dji-flight"
     xmlns="http://www.topografix.com/GPX/1/1">
  <trk>
    <name>%s</name>
    <trkseg>
`, fd.FileName)

	for _, fr := range fd.Frames {
		if fr.Latitude == 0 && fr.Longitude == 0 {
			continue
		}
		ts := fr.Timestamp.Format("2006-01-02T15:04:05Z")
		fmt.Fprintf(f, `      <trkpt lat="%f" lon="%f">
        <ele>%f</ele>
        <time>%s</time>
      </trkpt>
`, fr.Latitude, fr.Longitude, fr.AbsAltitude, ts)
	}

	fmt.Fprint(f, `    </trkseg>
  </trk>
</gpx>
`)
	return nil
}

// ToGeoJSON writes the flight path as a GeoJSON FeatureCollection with
// a LineString for the path and Point features for start/end.
func ToGeoJSON(fd *parser.FlightData, path string) error {
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
				"type": "Feature",
				"properties": map[string]any{
					"name":         fd.FileName,
					"duration_s":   fd.DurationSeconds,
					"distance_m":   fd.TotalDistanceM,
					"max_alt_m":    fd.MaxAltitudeM,
					"max_speed_ms": fd.MaxSpeedMs,
				},
				"geometry": map[string]any{
					"type":        "LineString",
					"coordinates": coords,
				},
			},
			makePoint("Start", fd.StartCoords[0], fd.StartCoords[1]),
			makePoint("End", fd.EndCoords[0], fd.EndCoords[1]),
		},
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(geojson)
}

func makePoint(name string, lat, lon float64) map[string]any {
	return map[string]any{
		"type":       "Feature",
		"properties": map[string]any{"name": name},
		"geometry": map[string]any{
			"type":        "Point",
			"coordinates": []float64{lon, lat},
		},
	}
}

func ff(v float64, prec int) string {
	return strconv.FormatFloat(v, 'f', prec, 64)
}
