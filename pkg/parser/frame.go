package parser

import "time"

// Frame represents a single telemetry data point from a DJI SRT file.
// Each video frame gets one of these — typically 30 per second.
type Frame struct {
	// SRT subtitle index (1, 2, 3, ...)
	Index int `json:"index"`

	// Timecodes from the SRT timeline
	StartTime time.Duration `json:"start_time_ms"`
	EndTime   time.Duration `json:"end_time_ms"`

	// DJI-specific counters
	FrameCount int `json:"frame_count"`
	DiffTimeMs int `json:"diff_time_ms"`

	// Wall clock timestamp from the drone
	Timestamp time.Time `json:"timestamp"`

	// ── Camera settings ──────────────────────────────
	ISO       int     `json:"iso"`
	Shutter   string  `json:"shutter"`   // e.g. "1/1250.0"
	FNum      float64 `json:"fnum"`      // aperture × 100 on older drones, or raw f-stop
	EV        float64 `json:"ev"`        // exposure value
	CT        int     `json:"ct"`        // color temperature in Kelvin
	ColorMode string  `json:"color_mode"`
	FocalLen  int     `json:"focal_len"` // focal length × 100

	// ── GPS & altitude ───────────────────────────────
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	RelAltitude float64 `json:"rel_alt"`  // meters above takeoff
	AbsAltitude float64 `json:"abs_alt"`  // meters above sea level

	// ── Gimbal orientation ───────────────────────────
	GimbalYaw   float64 `json:"gb_yaw"`
	GimbalPitch float64 `json:"gb_pitch"`
	GimbalRoll  float64 `json:"gb_roll"`

	// ── Movement ─────────────────────────────────────
	Distance float64 `json:"distance"` // distance from home in meters
	Speed    float64 `json:"speed"`    // speed in m/s (if provided by drone)

	// ── Home point (some drones) ─────────────────────
	HomeLat float64 `json:"home_lat"`
	HomeLon float64 `json:"home_lon"`

	// ── Computed fields (filled after parsing) ───────
	HSpeed   float64 `json:"h_speed"`   // horizontal speed m/s (computed from GPS)
	VSpeed   float64 `json:"v_speed"`   // vertical speed m/s (computed from altitude delta)
	Bearing  float64 `json:"bearing"`   // heading in degrees (computed from GPS movement)

	// ── EIS / digital stabilization ──────────────────
	EISShiftX   float64 `json:"eis_shift_x"`   // digital EIS crop shift X (pixels or fraction)
	EISShiftY   float64 `json:"eis_shift_y"`   // digital EIS crop shift Y
	EISShiftMag float64 `json:"eis_shift_mag"` // magnitude sqrt(x²+y²) — turbulence proxy
	EISStrain   float64 `json:"eis_strain"`    // angle between pp_target and pp_current (degrees)

	// All raw key-value pairs we found — useful for
	// fields we don't explicitly model yet
	RawFields map[string]string `json:"raw_fields,omitempty"`
}

// FlightData holds the complete parsed result from one SRT file.
type FlightData struct {
	FileName string  `json:"file_name"`
	Frames   []Frame `json:"frames"`

	// ── Summary stats (computed after parsing) ───────
	DurationSeconds  float64    `json:"duration_seconds"`
	TotalDistanceM   float64    `json:"total_distance_m"`
	MaxAltitudeM     float64    `json:"max_altitude_m"`
	MaxSpeedMs       float64    `json:"max_speed_ms"`
	AvgSpeedMs       float64    `json:"avg_speed_ms"`
	StartCoords      [2]float64 `json:"start_coords"` // [lat, lon]
	EndCoords        [2]float64 `json:"end_coords"`
	StartTime        time.Time  `json:"start_time"`
	EndTime          time.Time  `json:"end_time"`
	FrameCount       int        `json:"frame_count"`

	// ── Enhanced metrics ─────────────────────────────
	AltitudeGainM       float64 `json:"altitude_gain_m"`
	AltitudeLossM       float64 `json:"altitude_loss_m"`
	MinRelAltitudeM     float64 `json:"min_rel_altitude_m"`
	MaxAltitudeFrameIdx int     `json:"max_altitude_frame_idx"`
	MaxSpeedFrameIdx    int     `json:"max_speed_frame_idx"`
	FlightDate          string  `json:"flight_date"` // "2024-01-15"

	// ── Hover / area analysis ─────────────────────────
	HoverTimeSeconds float64 `json:"hover_time_seconds"` // seconds with HSpeed < 0.3 m/s
	SurveyedAreaM2   float64 `json:"surveyed_area_m2"`   // convex hull of GPS track

	// ── Lighting / camera analysis ───────────────────
	AvgColorTempK float64 `json:"avg_color_temp_k"` // mean color temperature (K)
	AvgEV         float64 `json:"avg_ev"`            // mean exposure value
	HighISOPct    float64 `json:"high_iso_pct"`      // % of frames with ISO > 800
	HighISOFrames int     `json:"high_iso_frames"`

	// ── Solar position ────────────────────────────────
	SolarElevationDeg float64 `json:"solar_elevation_deg"` // sun angle at takeoff
	GoldenHour        bool    `json:"golden_hour"`         // sun within 6° of horizon

	// ── EIS / digital stabilization ───────────────────
	EISAnalysis EISAnalysis `json:"eis_analysis"`

	// ── Wind estimation ──────────────────────────────
	EstimatedWindDir   float64 `json:"est_wind_dir_deg,omitempty"`
	EstimatedWindSpeed float64 `json:"est_wind_speed_ms,omitempty"`

	// ── Stabilization analysis ───────────────────────
	GimbalStability GimbalStats `json:"gimbal_stability"`
}

// EISAnalysis summarises the Electronic Image Stabilisation workload.
type EISAnalysis struct {
	AvgStrainDeg     float64 `json:"avg_strain_deg"`     // mean quaternion error between target and current
	MaxStrainDeg     float64 `json:"max_strain_deg"`     // peak quaternion error
	OverBorderEvents int     `json:"over_border_events"` // frames where EIS hit image boundary
	AvgShiftMag      float64 `json:"avg_shift_mag"`      // mean digital shift magnitude
	MaxShiftMag      float64 `json:"max_shift_mag"`      // peak digital shift
}

// GimbalStats captures how much the gimbal had to work to
// keep the image stable. High variance = windy / turbulent flight.
type GimbalStats struct {
	AvgPitch      float64 `json:"avg_pitch"`
	AvgRoll       float64 `json:"avg_roll"`
	PitchVariance float64 `json:"pitch_variance"`
	RollVariance  float64 `json:"roll_variance"`
	MaxPitchDelta float64 `json:"max_pitch_delta"` // biggest frame-to-frame pitch change
	MaxRollDelta  float64 `json:"max_roll_delta"`
}
