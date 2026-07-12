package session

import (
	"testing"

	"github.com/computerscienceiscool/voice-inventory/asr"
	"github.com/computerscienceiscool/voice-inventory/config"
	"github.com/computerscienceiscool/voice-inventory/store"
)

// FuzzSessionOps drives the capture state machine with arbitrary operation
// sequences and checks its invariants: no panics, states stay legal, and
// the reviewing state always has a pending record.
func FuzzSessionOps(f *testing.F) {
	f.Add([]byte{0, 2, 3, 4, 6}) // arm, begin, feed, end, confirm
	f.Add([]byte{0, 5, 7, 5, 8}) // arm, transcript, scratch, transcript, correct
	f.Add([]byte{0, 1, 0, 2, 4, 2, 3, 4, 6, 7})
	f.Add([]byte{5, 6, 7, 8, 9, 10, 11})

	transcripts := []string{
		"Twelve boxes of RJ45 connectors in bin A-14",
		"yes",
		"no, A-40",
		"scratch that",
		"several hundred bolts",
		"location is B-2",
		"",
	}

	f.Fuzz(func(t *testing.T, ops []byte) {
		if len(ops) > 64 {
			ops = ops[:64]
		}
		st, err := store.OpenMemory()
		if err != nil {
			t.Skip()
		}
		defer st.Close()
		cfg := config.Default()
		cfg.DeviceID = "fuzz"
		mock := &asr.Mock{Results: []asr.Result{
			asr.TextResult(transcripts[0], "en", 0.9),
		}}
		s, err := New(cfg, Deps{Store: st, Transcriber: mock})
		if err != nil {
			t.Fatal(err)
		}
		chunk := make([]float32, 4800)
		for i := range chunk {
			if i%36 < 18 { // crude 440ish square wave, loud enough for VAD
				chunk[i] = 0.3
			} else {
				chunk[i] = -0.3
			}
		}
		ti := 0
		for _, op := range ops {
			switch op % 12 {
			case 0:
				s.Arm()
			case 1:
				s.Disarm()
			case 2:
				_ = s.BeginUtterance()
			case 3:
				s.FeedPCM(chunk, 16000, 1)
			case 4:
				s.EndUtterance()
			case 5:
				text := transcripts[ti%len(transcripts)]
				ti++
				s.HandleTranscript(text, asr.Result{Text: text, Language: "en", Confidence: 0.9}, nil)
			case 6:
				_ = s.Confirm()
			case 7:
				s.Scratch()
			case 8:
				_ = s.CorrectField("location", "C-7")
			case 9:
				_ = s.CorrectField("quantity", "fifteen")
			case 10:
				_ = s.RefreshRefData()
			case 11:
				if p := s.Pending(); p != nil {
					// batch review races the live session
					if op%2 == 0 {
						_ = st.Reject(p.ID)
					} else {
						_ = st.Confirm(p.ID)
					}
				}
			}
			state := s.State()
			switch state {
			case StateIdle, StateArmed, StateReviewing:
			default:
				t.Fatalf("illegal state %q after op %d", state, op%12)
			}
			if state == StateReviewing && s.Pending() == nil {
				t.Fatalf("reviewing with no pending record after op %d", op%12)
			}
		}
	})
}
