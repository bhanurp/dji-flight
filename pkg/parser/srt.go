package parser

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// ParseFile reads an SRT file from disk and returns parsed FlightData.
func ParseFile(path string) (*FlightData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	fd, err := Parse(f, path)
	if err != nil {
		return nil, err
	}
	return fd, nil
}

// Parse reads SRT data from any reader. fileName is used for labeling only.
func Parse(r io.Reader, fileName string) (*FlightData, error) {
	blocks := splitIntoBlocks(r)

	fd := &FlightData{
		FileName: fileName,
		Frames:   make([]Frame, 0, len(blocks)),
	}

	for _, block := range blocks {
		frame, err := parseBlock(block)
		if err != nil {
			// Skip malformed blocks rather than failing entirely.
			// Real-world SRT files often have incomplete final blocks.
			continue
		}
		fd.Frames = append(fd.Frames, frame)
	}

	if len(fd.Frames) == 0 {
		return nil, fmt.Errorf("no valid frames found in %s", fileName)
	}

	computeDerivedFields(fd)
	return fd, nil
}

// ── Block splitting ─────────────────────────────────────────────
//
// An SRT file is a sequence of "blocks" separated by blank lines:
//
//   1                              ← subtitle index
//   00:00:00,000 --> 00:00:00,033  ← timecode range
//   <font size="28">FrameCnt: ...  ← telemetry lines (may span multiple)
//   ...
//   </font>
//                                  ← blank line = end of block

type rawBlock struct {
	lines []string
}

func splitIntoBlocks(r io.Reader) []rawBlock {
	scanner := bufio.NewScanner(r)
	// Increase buffer for long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var blocks []rawBlock
	var current []string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			if len(current) > 0 {
				blocks = append(blocks, rawBlock{lines: current})
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	// Don't forget the last block if file doesn't end with blank line
	if len(current) > 0 {
		blocks = append(blocks, rawBlock{lines: current})
	}

	return blocks
}

// ── Single block parsing ────────────────────────────────────────

func parseBlock(b rawBlock) (Frame, error) {
	if len(b.lines) < 2 {
		return Frame{}, fmt.Errorf("block too short")
	}

	var frame Frame
	frame.RawFields = make(map[string]string)

	// Line 0: subtitle index (e.g. "1")
	idx, err := strconv.Atoi(b.lines[0])
	if err != nil {
		return frame, fmt.Errorf("invalid index: %s", b.lines[0])
	}
	frame.Index = idx

	// Line 1: timecode range (e.g. "00:00:00,000 --> 00:00:01,000")
	frame.StartTime, frame.EndTime = parseTimecodeRange(b.lines[1])

	// Lines 2+: telemetry data
	// Join all remaining lines into one big string for bracket parsing.
	// This handles cases where data spans multiple lines.
	telemetry := strings.Join(b.lines[2:], " ")

	// Strip <font ...> and </font> tags — just remove everything
	// between < and > using simple scanning
	telemetry = stripHTMLTags(telemetry)

	// Parse the comma-separated header fields: "FrameCnt: 1, DiffTime: 33ms"
	parseHeaderFields(&frame, telemetry)

	// Parse the timestamp: "2026-01-30 09:58:21.637"
	parseTimestamp(&frame, telemetry)

	// Parse all [key: value] bracket pairs — this is the core trick.
	// No regex needed: split on "[", then for each piece, split on "]"
	// to isolate the key-value content.
	parseBracketFields(&frame, telemetry)

	return frame, nil
}

// ── Timecode parsing ────────────────────────────────────────────
//
// Format: "00:00:00,000 --> 00:00:01,000"
// We split on " --> " and parse each side.

func parseTimecodeRange(line string) (start, end time.Duration) {
	parts := strings.Split(line, " --> ")
	if len(parts) != 2 {
		return 0, 0
	}
	start = parseTimecode(strings.TrimSpace(parts[0]))
	end = parseTimecode(strings.TrimSpace(parts[1]))
	return
}

func parseTimecode(tc string) time.Duration {
	// "00:00:01,500" → split on ":" for h/m/s, then split s on "," for ms
	colonParts := strings.Split(tc, ":")
	if len(colonParts) != 3 {
		return 0
	}
	h := toInt(colonParts[0])
	m := toInt(colonParts[1])

	secAndMs := strings.Split(colonParts[2], ",")
	s := toInt(secAndMs[0])
	ms := 0
	if len(secAndMs) > 1 {
		ms = toInt(secAndMs[1])
	}

	return time.Duration(h)*time.Hour +
		time.Duration(m)*time.Minute +
		time.Duration(s)*time.Second +
		time.Duration(ms)*time.Millisecond
}

// ── HTML tag stripping (no regex) ───────────────────────────────

func stripHTMLTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, ch := range s {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(ch)
		}
	}
	return b.String()
}

// ── Header field parsing ────────────────────────────────────────
//
// Looks for "FrameCnt: 1, DiffTime: 33ms" in the telemetry string.
// Simple approach: find "FrameCnt:" and "DiffTime:" and read the
// next word after each.

func parseHeaderFields(frame *Frame, text string) {
	if i := strings.Index(text, "FrameCnt:"); i >= 0 {
		val := extractNextWord(text[i+len("FrameCnt:"):])
		val = strings.TrimRight(val, ",")
		frame.FrameCount = toInt(val)
	}
	if i := strings.Index(text, "DiffTime:"); i >= 0 {
		val := extractNextWord(text[i+len("DiffTime:"):])
		val = strings.TrimRight(val, "ms,")
		frame.DiffTimeMs = toInt(val)
	}
	if i := strings.Index(text, "SrtCnt:"); i >= 0 {
		val := extractNextWord(text[i+len("SrtCnt:"):])
		val = strings.TrimRight(val, ",")
		frame.FrameCount = toInt(val)
	}
}

// ── Timestamp parsing ───────────────────────────────────────────
//
// DJI timestamps look like: "2026-01-30 09:58:21.637"
// or sometimes: "2021-04-18 14:32:59,061,926"
// We find a YYYY-MM-DD pattern by scanning for 4-digit year followed by dash.

func parseTimestamp(frame *Frame, text string) {
	// Walk through the text looking for a 4-digit year pattern
	for i := 0; i <= len(text)-19; i++ {
		ch := text[i]
		if ch < '0' || ch > '9' {
			continue
		}
		// Check if we have YYYY-MM-DD HH:MM:SS
		if i+18 < len(text) && text[i+4] == '-' && text[i+7] == '-' && text[i+10] == ' ' && text[i+13] == ':' && text[i+16] == ':' {
			dateStr := text[i : i+19]
			// Try parsing with milliseconds first
			end := i + 19
			if end < len(text) && text[end] == '.' {
				// Find end of milliseconds
				msEnd := end + 1
				for msEnd < len(text) && text[msEnd] >= '0' && text[msEnd] <= '9' {
					msEnd++
				}
				dateStr = text[i:msEnd]
			}

			layouts := []string{
				"2006-01-02 15:04:05.000",
				"2006-01-02 15:04:05",
			}
			for _, layout := range layouts {
				if t, err := time.Parse(layout, dateStr); err == nil {
					frame.Timestamp = t
					return
				}
			}
		}
	}
}

// ── Bracket field parsing ───────────────────────────────────────
//
// This is the key insight that avoids regex entirely.
//
// DJI SRT data uses [key: value] format:
//   [iso: 100] [shutter: 1/1250.0] [latitude: 17.385]
//
// Algorithm:
//   1. Split the whole string on "["
//   2. For each piece, if it contains "]", take everything before "]"
//      → that's the "key: value" content
//   3. Split that on ":" → key is the first part, value is the rest
//   4. Map known keys to struct fields

func parseBracketFields(frame *Frame, text string) {
	segments := strings.Split(text, "[")

	for _, seg := range segments {
		closingIdx := strings.Index(seg, "]")
		if closingIdx < 0 {
			continue
		}

		// Everything before "]" is our key-value content
		kv := seg[:closingIdx]

		// Split on first ":" only — value might contain colons
		colonIdx := strings.Index(kv, ":")
		if colonIdx < 0 {
			// Some fields use "key value" format without colon (e.g. "rel_alt 57.200 abs_alt 204.644")
			parseBracketWithoutColon(frame, kv)
			continue
		}

		key := strings.TrimSpace(kv[:colonIdx])
		rawVal := strings.TrimSpace(kv[colonIdx+1:])

		// rawVal may contain additional key-value pairs in the same bracket,
		// e.g. "7.300 abs_alt: 509.222". Split on the first whitespace to get
		// the actual value, then process the remainder as more pairs.
		val := rawVal
		remainder := ""
		if spaceIdx := strings.IndexAny(rawVal, " \t"); spaceIdx >= 0 {
			val = rawVal[:spaceIdx]
			remainder = strings.TrimSpace(rawVal[spaceIdx+1:])
		}

		// Normalize key to lowercase for matching
		keyLower := strings.ToLower(key)

		// Store raw field
		frame.RawFields[keyLower] = val

		// Map to typed struct fields
		switch keyLower {
		case "iso":
			frame.ISO = toInt(val)
		case "shutter":
			frame.Shutter = val
		case "fnum":
			frame.FNum = toFloat(val)
		case "ev":
			frame.EV = toFloat(val)
		case "ct":
			frame.CT = toInt(val)
		case "color_md":
			frame.ColorMode = val
		case "focal_len":
			frame.FocalLen = toInt(val)
		case "latitude":
			frame.Latitude = toFloat(val)
		case "longitude":
			frame.Longitude = toFloat(val)
		case "longtitude": // DJI's typo in older firmware
			frame.Longitude = toFloat(val)
		case "rel_alt":
			frame.RelAltitude = toFloat(val)
		case "abs_alt":
			frame.AbsAltitude = toFloat(val)
		case "altitude":
			frame.RelAltitude = toFloat(val)
		case "gb_yaw":
			frame.GimbalYaw = toFloat(val)
		case "gb_pitch":
			frame.GimbalPitch = toFloat(val)
		case "gb_roll":
			frame.GimbalRoll = toFloat(val)
		case "distance":
			frame.Distance = toFloat(val)
		case "speed":
			frame.Speed = toFloat(val)
		case "shift x":
			// Format: [shift x: 0.00, y: 0.00]
			// val = "0.00", remainder = "y: 0.00" — but remainder processing
			// happens below. Store X here and Y via remainder recursion.
			frame.EISShiftX = toFloat(val)
			// Override remainder parsing: extract Y from "y: <num>"
			if strings.HasPrefix(remainder, "y:") || strings.HasPrefix(remainder, "y :") {
				yParts := strings.SplitN(remainder, ":", 2)
				if len(yParts) == 2 {
					frame.EISShiftY = toFloat(yParts[1])
					remainder = "" // consumed
				}
			}
		case "pp_target", "pp_current":
			// Quaternion: "w, x, y, z" — store the full rawVal, not just first token
			frame.RawFields[keyLower] = rawVal
			remainder = "" // consumed, don't re-process comma-separated floats
		case "pp_over_image_border":
			if val == "1" {
				frame.RawFields["pp_over_image_border"] = "1"
			}
		case "home":
			// Some formats: [home: 17.385, 78.486]
			coords := strings.Split(val, ",")
			if len(coords) >= 2 {
				frame.HomeLat = toFloat(strings.TrimSpace(coords[0]))
				frame.HomeLon = toFloat(strings.TrimSpace(coords[1]))
			}
		}

		// If the bracket had more content after the first value (e.g. "abs_alt: 509.222"),
		// recurse to parse those additional pairs within the same bracket.
		if remainder != "" {
			parseBracketFields(frame, "["+remainder+"]")
		}
	}
}

// parseBracketWithoutColon handles bracket content like:
// "rel_alt 57.200 abs_alt 204.644"
// Where multiple key-value pairs are space-separated without colons.
func parseBracketWithoutColon(frame *Frame, content string) {
	words := strings.Fields(content)
	for i := 0; i < len(words)-1; i += 2 {
		key := strings.ToLower(words[i])
		val := words[i+1]

		frame.RawFields[key] = val

		switch key {
		case "rel_alt":
			frame.RelAltitude = toFloat(val)
		case "abs_alt":
			frame.AbsAltitude = toFloat(val)
		case "latitude":
			frame.Latitude = toFloat(val)
		case "longitude":
			frame.Longitude = toFloat(val)
		}
	}
}

// ── Derived field computation ───────────────────────────────────

func computeDerivedFields(fd *FlightData) {
	frames := fd.Frames
	n := len(frames)
	if n == 0 {
		return
	}

	fd.FrameCount = n
	fd.StartTime = frames[0].Timestamp
	fd.EndTime = frames[n-1].Timestamp
	fd.StartCoords = [2]float64{frames[0].Latitude, frames[0].Longitude}
	fd.EndCoords = [2]float64{frames[n-1].Latitude, frames[n-1].Longitude}

	// Duration from the SRT timeline
	fd.DurationSeconds = frames[n-1].EndTime.Seconds()
	// Or from wall clock if available
	if !frames[0].Timestamp.IsZero() && !frames[n-1].Timestamp.IsZero() {
		wallDuration := frames[n-1].Timestamp.Sub(frames[0].Timestamp).Seconds()
		if wallDuration > 0 {
			fd.DurationSeconds = wallDuration
		}
	}

	// Compute per-frame speeds and accumulate stats.
	//
	// GPS on DJI drones updates at ~5-10 Hz while video runs at 30-60 fps.
	// Many consecutive frames share identical coordinates. Computing speed
	// frame-by-frame gives absurdly high values when the coordinate finally
	// ticks (e.g. 11 cm / 16 ms = 7 m/s instead of the real ~1 m/s).
	//
	// Fix: track the last frame where GPS actually changed. When it changes,
	// compute speed using the full elapsed time since that last change, then
	// propagate that speed to all intermediate frames.
	var totalDist float64
	var maxAlt, maxSpeed float64
	var totalSpeed float64
	var speedSamples int
	var altGain, altLoss float64
	minAlt := math.MaxFloat64
	maxAltIdx, maxSpeedIdx := 0, 0

	lastGPSChangeIdx := 0 // index of the frame where GPS last changed

	for i := range frames {
		// Max / min altitude
		if frames[i].RelAltitude > maxAlt {
			maxAlt = frames[i].RelAltitude
			maxAltIdx = i
		}
		if frames[i].RelAltitude < minAlt {
			minAlt = frames[i].RelAltitude
		}

		if i == 0 {
			continue
		}

		prev := &frames[i-1]
		curr := &frames[i]

		// Time delta between frames
		dtSec := curr.EndTime.Seconds() - prev.EndTime.Seconds()
		if dtSec <= 0 {
			dtSec = float64(curr.DiffTimeMs) / 1000.0
		}
		if dtSec <= 0 {
			dtSec = 0.033 // default ~30fps
		}

		// Vertical speed (altitude updates every frame, use frame delta)
		if dtSec > 0 {
			curr.VSpeed = (curr.RelAltitude - prev.RelAltitude) / dtSec
		}

		// Altitude gain / loss — accumulate every frame regardless of GPS
		altDelta := curr.RelAltitude - prev.RelAltitude
		if altDelta > 0 {
			altGain += altDelta
		} else {
			altLoss += -altDelta
		}

		// GPS fix check
		validGPS := (curr.Latitude != 0 || curr.Longitude != 0)
		if !validGPS {
			continue
		}

		gpsChanged := curr.Latitude != prev.Latitude || curr.Longitude != prev.Longitude

		if !gpsChanged {
			// Carry forward the speed from the last GPS update
			curr.HSpeed = prev.HSpeed
			curr.Bearing = prev.Bearing
		} else {
			// GPS changed — compute speed over the full interval since last change
			ref := &frames[lastGPSChangeIdx]
			dist := haversineM(ref.Latitude, ref.Longitude, curr.Latitude, curr.Longitude)
			elapsed := curr.EndTime.Seconds() - ref.EndTime.Seconds()
			if elapsed <= 0 {
				elapsed = float64(i-lastGPSChangeIdx) * 0.033
			}

			var hspd float64
			if elapsed > 0 {
				hspd = dist / elapsed
			}

			// Sanity cap: DJI drones top out ~90 km/h (25 m/s).
			// Anything above 40 m/s (144 km/h) is GPS error — discard.
			const maxPlausibleMs = 40.0
			if hspd <= maxPlausibleMs {
				totalDist += dist
				// Back-fill speed to all frames since last GPS change
				for j := lastGPSChangeIdx + 1; j <= i; j++ {
					frames[j].HSpeed = hspd
				}
			}

			curr.Bearing = bearing(ref.Latitude, ref.Longitude, curr.Latitude, curr.Longitude)
			// Back-fill bearing too
			for j := lastGPSChangeIdx + 1; j < i; j++ {
				frames[j].Bearing = curr.Bearing
			}

			lastGPSChangeIdx = i
		}

		// Track max speed (use drone-reported speed if available, otherwise computed)
		spd := curr.Speed
		if spd == 0 {
			spd = curr.HSpeed
		}
		if spd > maxSpeed {
			maxSpeed = spd
			maxSpeedIdx = i
		}
		totalSpeed += spd
		speedSamples++
	}

	fd.TotalDistanceM = totalDist
	fd.MaxAltitudeM = maxAlt
	fd.MaxSpeedMs = maxSpeed
	if speedSamples > 0 {
		fd.AvgSpeedMs = totalSpeed / float64(speedSamples)
	}

	fd.AltitudeGainM = altGain
	fd.AltitudeLossM = altLoss
	if minAlt == math.MaxFloat64 {
		minAlt = 0
	}
	fd.MinRelAltitudeM = minAlt
	fd.MaxAltitudeFrameIdx = maxAltIdx
	fd.MaxSpeedFrameIdx = maxSpeedIdx

	// Flight date from first valid timestamp
	for _, f := range frames {
		if !f.Timestamp.IsZero() {
			fd.FlightDate = f.Timestamp.Format("2006-01-02")
			break
		}
	}

	computeGimbalStats(fd)
	estimateWind(fd)
	computeEISAnalysis(fd)
	computeHoverTime(fd)
	computeSurveyedArea(fd)
	computeSolarPosition(fd)
	computeCameraStats(fd)
}

// ── Gimbal stability analysis ───────────────────────────────────
//
// The gimbal compensates for wind and vibration. By looking at how
// much gb_pitch and gb_roll vary frame-to-frame, we can estimate
// how turbulent the flight was.

func computeGimbalStats(fd *FlightData) {
	frames := fd.Frames
	n := len(frames)
	if n < 2 {
		return
	}

	var sumPitch, sumRoll float64
	for i := range frames {
		sumPitch += frames[i].GimbalPitch
		sumRoll += frames[i].GimbalRoll
	}
	avgPitch := sumPitch / float64(n)
	avgRoll := sumRoll / float64(n)

	var variancePitch, varianceRoll float64
	var maxPitchDelta, maxRollDelta float64

	for i := range frames {
		dp := frames[i].GimbalPitch - avgPitch
		dr := frames[i].GimbalRoll - avgRoll
		variancePitch += dp * dp
		varianceRoll += dr * dr

		if i > 0 {
			pd := math.Abs(frames[i].GimbalPitch - frames[i-1].GimbalPitch)
			rd := math.Abs(frames[i].GimbalRoll - frames[i-1].GimbalRoll)
			if pd > maxPitchDelta {
				maxPitchDelta = pd
			}
			if rd > maxRollDelta {
				maxRollDelta = rd
			}
		}
	}

	fd.GimbalStability = GimbalStats{
		AvgPitch:      avgPitch,
		AvgRoll:       avgRoll,
		PitchVariance: variancePitch / float64(n),
		RollVariance:  varianceRoll / float64(n),
		MaxPitchDelta: maxPitchDelta,
		MaxRollDelta:  maxRollDelta,
	}
}

// ── Wind estimation ─────────────────────────────────────────────
//
// Crude but useful: when flying in different directions, wind
// causes asymmetric ground speeds. If the drone flies a loop or
// covers multiple headings, we can estimate wind by comparing
// ground speed in opposite directions.
//
// We bucket frames by 8 compass directions, compute average speed
// in each, and the direction with lowest avg speed is roughly
// the wind direction (flying into wind = slow).

func estimateWind(fd *FlightData) {
	if len(fd.Frames) < 30 {
		return
	}

	// 8 compass buckets: N, NE, E, SE, S, SW, W, NW
	var bucketSpeed [8]float64
	var bucketCount [8]int

	for i := 1; i < len(fd.Frames); i++ {
		f := &fd.Frames[i]
		if f.HSpeed < 0.5 { // ignore near-stationary frames
			continue
		}
		bucket := int(math.Mod(f.Bearing+22.5, 360) / 45.0)
		if bucket < 0 {
			bucket += 8
		}
		if bucket >= 8 {
			bucket = 0
		}
		bucketSpeed[bucket] += f.HSpeed
		bucketCount[bucket]++
	}

	// Need data in at least 3 different directions
	populated := 0
	for _, c := range bucketCount {
		if c > 0 {
			populated++
		}
	}
	if populated < 3 {
		return
	}

	// Find direction with minimum average speed (headwind direction)
	// and max average speed (tailwind direction)
	minAvg := math.MaxFloat64
	maxAvg := 0.0
	minDir := 0

	for i := 0; i < 8; i++ {
		if bucketCount[i] == 0 {
			continue
		}
		avg := bucketSpeed[i] / float64(bucketCount[i])
		if avg < minAvg {
			minAvg = avg
			minDir = i
		}
		if avg > maxAvg {
			maxAvg = avg
		}
	}

	// Wind comes from the direction the drone is slowest
	// (because it's fighting headwind)
	fd.EstimatedWindDir = float64(minDir) * 45.0
	// Speed difference between tailwind and headwind is ~2× wind speed
	if maxAvg > minAvg {
		fd.EstimatedWindSpeed = (maxAvg - minAvg) / 2.0
	}
}

// ── Haversine & bearing ─────────────────────────────────────────

func haversineM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusM = 6_371_000.0
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}

func bearing(lat1, lon1, lat2, lon2 float64) float64 {
	dLon := toRad(lon2 - lon1)
	y := math.Sin(dLon) * math.Cos(toRad(lat2))
	x := math.Cos(toRad(lat1))*math.Sin(toRad(lat2)) -
		math.Sin(toRad(lat1))*math.Cos(toRad(lat2))*math.Cos(dLon)
	deg := toDeg(math.Atan2(y, x))
	return math.Mod(deg+360, 360)
}

func toRad(d float64) float64 { return d * math.Pi / 180 }
func toDeg(r float64) float64 { return r * 180 / math.Pi }

// ── Tiny helpers ────────────────────────────────────────────────

func toInt(s string) int {
	s = strings.TrimSpace(s)
	v, _ := strconv.Atoi(s)
	return v
}

func toFloat(s string) float64 {
	s = strings.TrimSpace(s)
	// Take only the first whitespace-delimited token — value may have trailing
	// key-value pairs in the same bracket (e.g. "7.300 abs_alt: 509.222")
	if i := strings.IndexAny(s, " \t\n\r"); i >= 0 {
		s = s[:i]
	}
	// Strip trailing commas (e.g. "0.00," from "shift x: 0.00, y: 0.00")
	s = strings.TrimRight(s, ",")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

func extractNextWord(s string) string {
	s = strings.TrimSpace(s)
	end := strings.IndexAny(s, " \t\n\r")
	if end < 0 {
		return s
	}
	return s[:end]
}

// ── EIS analysis ────────────────────────────────────────────────

func computeEISAnalysis(fd *FlightData) {
	frames := fd.Frames
	var sumStrain, maxStrain float64
	var sumShift, maxShift float64
	var strainSamples, shiftSamples int
	var overBorder int

	for i := range frames {
		f := &frames[i]

		// EIS shift magnitude
		f.EISShiftMag = math.Sqrt(f.EISShiftX*f.EISShiftX + f.EISShiftY*f.EISShiftY)
		if f.EISShiftX != 0 || f.EISShiftY != 0 {
			sumShift += f.EISShiftMag
			if f.EISShiftMag > maxShift {
				maxShift = f.EISShiftMag
			}
			shiftSamples++
		}

		// Quaternion strain from pp_target vs pp_current
		target := f.RawFields["pp_target"]
		current := f.RawFields["pp_current"]
		if target != "" && current != "" {
			tw, tx, ty, tz := parseQuaternion(target)
			cw, cx, cy, cz := parseQuaternion(current)
			strain := quaternionAngleDeg(tw, tx, ty, tz, cw, cx, cy, cz)
			f.EISStrain = strain
			sumStrain += strain
			if strain > maxStrain {
				maxStrain = strain
			}
			strainSamples++
		}

		// Over-border events
		if f.RawFields["pp_over_image_border"] == "1" {
			overBorder++
		}
	}

	var avgStrain, avgShift float64
	if strainSamples > 0 {
		avgStrain = sumStrain / float64(strainSamples)
	}
	if shiftSamples > 0 {
		avgShift = sumShift / float64(shiftSamples)
	}

	fd.EISAnalysis = EISAnalysis{
		AvgStrainDeg:     avgStrain,
		MaxStrainDeg:     maxStrain,
		OverBorderEvents: overBorder,
		AvgShiftMag:      avgShift,
		MaxShiftMag:      maxShift,
	}
}

func parseQuaternion(s string) (w, x, y, z float64) {
	parts := strings.Split(s, ",")
	vals := make([]float64, 4)
	for i, p := range parts {
		if i >= 4 {
			break
		}
		vals[i], _ = strconv.ParseFloat(strings.TrimSpace(p), 64)
	}
	return vals[0], vals[1], vals[2], vals[3]
}

// quaternionAngleDeg returns the angle (degrees) between two unit quaternions.
func quaternionAngleDeg(w1, x1, y1, z1, w2, x2, y2, z2 float64) float64 {
	dot := w1*w2 + x1*x2 + y1*y2 + z1*z2
	// q and -q represent the same rotation — take the shorter arc
	if dot < 0 {
		dot = -dot
	}
	if dot > 1.0 {
		dot = 1.0
	}
	return toDeg(2 * math.Acos(dot))
}

// ── Hover time ──────────────────────────────────────────────────

func computeHoverTime(fd *FlightData) {
	var hover float64
	for i := range fd.Frames {
		f := &fd.Frames[i]
		if f.HSpeed < 0.3 && (f.Latitude != 0 || f.Longitude != 0) {
			// Frame duration from DiffTimeMs, or assume 33ms
			dt := float64(f.DiffTimeMs) / 1000.0
			if dt <= 0 {
				dt = 0.033
			}
			hover += dt
		}
	}
	fd.HoverTimeSeconds = hover
}

// ── Surveyed area (convex hull) ─────────────────────────────────

type gpsPoint struct{ lat, lon float64 }

func computeSurveyedArea(fd *FlightData) {
	var pts []gpsPoint
	seen := map[gpsPoint]bool{}
	for _, f := range fd.Frames {
		if f.Latitude == 0 && f.Longitude == 0 {
			continue
		}
		// Round to ~11m precision to deduplicate GPS-frozen frames
		p := gpsPoint{
			math.Round(f.Latitude*1e4) / 1e4,
			math.Round(f.Longitude*1e4) / 1e4,
		}
		if !seen[p] {
			pts = append(pts, p)
			seen[p] = true
		}
	}
	if len(pts) < 3 {
		return
	}

	hull := convexHull(pts)
	if len(hull) < 3 {
		return
	}

	// Shoelace formula in metric coordinates
	avgLat := 0.0
	for _, p := range hull {
		avgLat += p.lat
	}
	avgLat /= float64(len(hull))

	mPerDegLat := 111320.0
	mPerDegLon := 111320.0 * math.Cos(toRad(avgLat))

	area := 0.0
	n := len(hull)
	for i := 0; i < n; i++ {
		j := (i + 1) % n
		xi := hull[i].lon * mPerDegLon
		yi := hull[i].lat * mPerDegLat
		xj := hull[j].lon * mPerDegLon
		yj := hull[j].lat * mPerDegLat
		area += xi*yj - xj*yi
	}
	fd.SurveyedAreaM2 = math.Abs(area) / 2
}

// convexHull computes the convex hull via gift wrapping (Jarvis march).
func convexHull(pts []gpsPoint) []gpsPoint {
	n := len(pts)
	if n < 3 {
		return pts
	}

	// Find leftmost point as starting anchor
	left := 0
	for i := 1; i < n; i++ {
		if pts[i].lon < pts[left].lon {
			left = i
		}
	}

	var hull []gpsPoint
	p := left
	for {
		hull = append(hull, pts[p])
		q := (p + 1) % n
		for i := 0; i < n; i++ {
			// Cross product: positive = pts[i] is more counter-clockwise
			if gpsCross(pts[p], pts[i], pts[q]) < 0 {
				q = i
			}
		}
		p = q
		if p == left || len(hull) > n {
			break
		}
	}
	return hull
}

func gpsCross(O, A, B gpsPoint) float64 {
	return (A.lon-O.lon)*(B.lat-O.lat) - (A.lat-O.lat)*(B.lon-O.lon)
}

// ── Solar position ──────────────────────────────────────────────
//
// NOAA simplified solar position algorithm.
// Returns sun elevation angle in degrees above horizon at the given
// UTC time and geographic coordinates.

func computeSolarPosition(fd *FlightData) {
	// Find first frame with valid timestamp and GPS
	for _, f := range fd.Frames {
		if f.Timestamp.IsZero() || (f.Latitude == 0 && f.Longitude == 0) {
			continue
		}
		elev := solarElevationDeg(f.Timestamp, f.Latitude, f.Longitude)
		fd.SolarElevationDeg = elev
		// Golden hour: sun within 6° above or below horizon
		fd.GoldenHour = elev >= -6 && elev <= 6
		return
	}
}

func solarElevationDeg(t time.Time, lat, lon float64) float64 {
	jd := julianDay(t)
	T := (jd - 2451545.0) / 36525.0

	// Geometric mean longitude (degrees)
	L0 := math.Mod(280.46646+T*(36000.76983+T*0.0003032), 360)
	// Geometric mean anomaly (degrees)
	M := math.Mod(357.52911+T*(35999.05029-T*0.0001537), 360)
	Mrad := toRad(M)

	// Equation of centre
	C := (1.914602-T*(0.004817+0.000014*T))*math.Sin(Mrad) +
		(0.019993-0.000101*T)*math.Sin(2*Mrad) +
		0.000289*math.Sin(3*Mrad)

	sunLon := L0 + C
	omega := 125.04 - 1934.136*T
	lambda := sunLon - 0.00569 - 0.00478*math.Sin(toRad(omega))

	// Obliquity of ecliptic (degrees)
	epsilon := 23.0 + (26.0+(21.448-T*(46.815+T*(0.00059-T*0.001813)))/60)/60
	epsilonCorr := epsilon + 0.00256*math.Cos(toRad(omega))

	// Declination
	dec := math.Asin(math.Sin(toRad(epsilonCorr)) * math.Sin(toRad(lambda)))

	// Equation of time (minutes)
	y := math.Pow(math.Tan(toRad(epsilonCorr/2)), 2)
	eOrbEcc := 0.016708634
	eqTime := 4 * toDeg(y*math.Sin(toRad(2*L0))-
		2*eOrbEcc*math.Sin(Mrad)+
		4*eOrbEcc*y*math.Sin(Mrad)*math.Cos(toRad(2*L0))-
		0.5*y*y*math.Sin(toRad(4*L0))-
		1.25*eOrbEcc*eOrbEcc*math.Sin(toRad(2*Mrad)))

	// True solar time (minutes)
	utcMin := float64(t.UTC().Hour()*60+t.UTC().Minute()) + float64(t.UTC().Second())/60.0
	trueSolarTime := math.Mod(utcMin+eqTime+4*lon, 1440)

	// Hour angle (degrees)
	ha := trueSolarTime/4 - 180
	if ha < -180 {
		ha += 360
	}

	// Elevation
	latRad := toRad(lat)
	elev := math.Asin(math.Sin(latRad)*math.Sin(dec) +
		math.Cos(latRad)*math.Cos(dec)*math.Cos(toRad(ha)))
	return toDeg(elev)
}

func julianDay(t time.Time) float64 {
	t = t.UTC()
	y := float64(t.Year())
	m := float64(t.Month())
	d := float64(t.Day()) +
		float64(t.Hour())/24.0 +
		float64(t.Minute())/1440.0 +
		float64(t.Second())/86400.0

	if m <= 2 {
		y--
		m += 12
	}
	A := math.Floor(y / 100)
	B := 2 - A + math.Floor(A/4)
	return math.Floor(365.25*(y+4716)) + math.Floor(30.6001*(m+1)) + d + B - 1524.5
}

// ── Camera / exposure analysis ──────────────────────────────────

func computeCameraStats(fd *FlightData) {
	var sumCT, sumEV float64
	var ctSamples, evSamples, highISO int

	for _, f := range fd.Frames {
		if f.CT > 0 {
			sumCT += float64(f.CT)
			ctSamples++
		}
		// EV is always present (can be 0 which is valid)
		sumEV += f.EV
		evSamples++

		if f.ISO > 800 {
			highISO++
		}
	}

	if ctSamples > 0 {
		fd.AvgColorTempK = sumCT / float64(ctSamples)
	}
	if evSamples > 0 {
		fd.AvgEV = sumEV / float64(evSamples)
	}
	if len(fd.Frames) > 0 {
		fd.HighISOFrames = highISO
		fd.HighISOPct = float64(highISO) / float64(len(fd.Frames)) * 100
	}
}
