package mbe

// VAD decides whether a frame is sent as a silence frame.  TIA-102.BABA-1
// defines the silence frame format but not the decision, so this is a simple
// energy detector: a frame is speech if it, or the next frame (from the
// encoder's look-ahead), exceeds both an absolute floor and a multiple of a
// tracked noise floor; speech is held for a few frames after it ends.  Its
// parameters were fitted to the silence decisions of a deployed AMBE+2
// encoder (≈88% frame agreement on held-out speech).
type VAD struct {
	ratio, floor, rise float64
	hangFrames         int

	noise float64
	init  bool
	hang  int
}

// NewVAD returns a detector with the parameters fitted to the MD-380 encoder.
func NewVAD() *VAD {
	return &VAD{ratio: 32, floor: 5000, rise: 1.001, hangFrames: 4}
}

// Silent reports whether the frame with mean-square energy cur (followed by
// a frame with energy next) is silence.
func (v *VAD) Silent(cur, next float64) bool {
	if !v.init || cur < v.noise {
		v.noise, v.init = cur, true
	} else {
		v.noise *= v.rise
	}
	th := v.noise * v.ratio
	if th < v.floor {
		th = v.floor
	}
	if cur > th || next > th {
		v.hang = v.hangFrames
		return false
	}
	if v.hang > 0 {
		v.hang--
		return false
	}
	return true
}
