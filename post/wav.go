package post

// The batch speech endpoint takes one format only: a RIFF/WAVE file, mono,
// 16-bit integer PCM, at 16 kHz or 24 kHz (dev.meta.ai/docs/speech-to-text).
// The app should record that format (the cookbook's ffmpeg line:
// -ac 1 -ar 24000 -c:a pcm_s16le).
//
// Reading: our own strict RIFF parser. github.com/go-audio/wav's reader
// trusts the chunk sizes in the header (it allocates a LIST or fmt chunk of
// the size the header claims, and its Duration comes from header fields), so
// a 64-byte file could take gigabytes (X35d review). Here every chunk must fit
// in the bytes received, and the length comes from the samples actually there.
// Writing: github.com/go-audio/wav's encoder (Apache-2.0).
//
// A file that already matches, with exact header sizes, is sent byte for
// byte. Any other PCM WAV (stereo, 8-48 kHz, 8/16/24/32-bit, or a streaming
// header with size 0 or 0xFFFFFFFF) is rewritten: mixed to mono, low-passed
// and resampled to 16 kHz when needed, 16-bit, with exact sizes.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/go-audio/audio"
	"github.com/go-audio/wav"
)

// AudioInfo describes the upload and what was sent on.
type AudioInfo struct {
	SampleRate int  `json:"sample_rate"`
	Channels   int  `json:"channels"`
	BitDepth   int  `json:"bit_depth"`
	MS         int  `json:"ms"`
	Converted  bool `json:"converted"` // false = the upload was sent byte for byte
}

// ErrNotWAV is every refusal of the audio (the message says why).
var ErrNotWAV = errors.New("audio must be a PCM WAV file")

// ErrTooLong is a WAV over the length limit.
var ErrTooLong = errors.New("audio too long")

// TargetRate is the rate a converted file is written at.
const TargetRate = 16000

// wavFormat is the parsed fmt chunk and the data chunk.
type wavFormat struct {
	format, channels, bits int
	rate                   int
	data                   []byte // the samples (whole frames only)
	exact                  bool   // the RIFF and data sizes match the file
}

// extensible PCM sub-format GUID (KSDATAFORMAT_SUBTYPE_PCM).
var pcmGUID = []byte{0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0xaa, 0x00, 0x38, 0x9b, 0x71}

// parseWAV reads the RIFF chunks without trusting any size: a chunk that does
// not fit is an error, except the data chunk, which streaming recorders write
// with size 0 or 0xFFFFFFFF; it then runs to the end of the file.
func parseWAV(b []byte) (wavFormat, error) {
	var f wavFormat
	bad := func(why string) (wavFormat, error) { return f, fmt.Errorf("%w (%s)", ErrNotWAV, why) }
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return bad("not a RIFF/WAVE file")
	}
	riffExact := int(binary.LittleEndian.Uint32(b[4:8])) == len(b)-8
	dataExact := false
	haveFmt := false
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := pos + 8
		left := len(b) - body
		if id == "data" {
			if !haveFmt {
				return bad("data before fmt")
			}
			n := size
			dataExact = size > 0 && size <= left
			if size == 0 || size > left {
				n = left
			}
			f.data = b[body : body+n]
			break
		}
		if size > left {
			return bad(fmt.Sprintf("chunk %q is larger than the file", id))
		}
		if id == "fmt " {
			if size < 16 || size > 40 {
				return bad(fmt.Sprintf("fmt chunk of %d bytes", size))
			}
			c := b[body : body+size]
			f.format = int(binary.LittleEndian.Uint16(c[0:2]))
			f.channels = int(binary.LittleEndian.Uint16(c[2:4]))
			f.rate = int(binary.LittleEndian.Uint32(c[4:8]))
			f.bits = int(binary.LittleEndian.Uint16(c[14:16]))
			if f.format == 0xFFFE { // WAVE_FORMAT_EXTENSIBLE: PCM only
				if size < 40 || string(c[24:40]) != string(pcmGUID) {
					return bad("extensible format that is not PCM")
				}
				f.format = 1
			}
			haveFmt = true
		}
		pos = body + size + size%2 // chunks are word-aligned
	}
	switch {
	case !haveFmt:
		return bad("no fmt chunk")
	case f.data == nil:
		return bad("no data chunk")
	case f.format != 1:
		return bad(fmt.Sprintf("format %d is not integer PCM", f.format))
	case f.channels < 1 || f.channels > 2:
		return bad(fmt.Sprintf("%d channels; send mono or stereo", f.channels))
	case f.rate < 8000 || f.rate > 48000:
		return bad(fmt.Sprintf("%d Hz; send 8-48 kHz", f.rate))
	case f.bits != 8 && f.bits != 16 && f.bits != 24 && f.bits != 32:
		return bad(fmt.Sprintf("%d-bit", f.bits))
	}
	frame := f.channels * f.bits / 8
	f.data = f.data[:len(f.data)/frame*frame]
	if len(f.data) == 0 {
		return bad("empty")
	}
	f.exact = riffExact && dataExact && len(f.data) == int(binary.LittleEndian.Uint32(b[pos+4:pos+8]))
	return f, nil
}

// NormalizeWAV returns a WAV the endpoint accepts. maxDur bounds the length,
// measured from the samples received (not from the header).
func NormalizeWAV(b []byte, maxDur time.Duration) ([]byte, AudioInfo, error) {
	var info AudioInfo
	f, err := parseWAV(b)
	if err != nil {
		return nil, info, err
	}
	info.SampleRate, info.Channels, info.BitDepth = f.rate, f.channels, f.bits
	frames := len(f.data) / (f.channels * f.bits / 8)
	dur := time.Duration(frames) * time.Second / time.Duration(f.rate)
	info.MS = int(dur / time.Millisecond)
	if maxDur > 0 && dur > maxDur {
		return nil, info, fmt.Errorf("%w (%.1f s; at most %.0f s)", ErrTooLong, dur.Seconds(), maxDur.Seconds())
	}
	if f.exact && f.channels == 1 && f.bits == 16 && (f.rate == 16000 || f.rate == 24000) {
		return b, info, nil
	}
	mono := toMonoFloat(f.data, f.channels, f.bits)
	rate := f.rate
	if rate != 16000 && rate != 24000 {
		mono, rate = resample(mono, f.rate, TargetRate), TargetRate
	}
	pcm := make([]int, len(mono))
	for i, v := range mono {
		pcm[i] = int(math.Round(math.Max(-1, math.Min(1, v)) * 32767))
	}
	ws := &memWS{}
	e := wav.NewEncoder(ws, rate, 16, 1, 1)
	if err := e.Write(&audio.IntBuffer{Data: pcm, Format: &audio.Format{NumChannels: 1, SampleRate: rate}, SourceBitDepth: 16}); err != nil {
		return nil, info, err
	}
	if err := e.Close(); err != nil {
		return nil, info, err
	}
	info.Converted = true
	return ws.buf, info, nil
}

// toMonoFloat reads little-endian PCM frames and averages the channels into
// [-1, 1] floats.
func toMonoFloat(data []byte, ch, bits int) []float64 {
	bps := bits / 8
	n := len(data) / (ch * bps)
	full := math.Pow(2, float64(bits-1))
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		s := 0.0
		for c := 0; c < ch; c++ {
			p := data[(i*ch+c)*bps:]
			var v float64
			switch bits {
			case 8: // 8-bit WAV is unsigned
				v = float64(int(p[0]) - 128)
			case 16:
				v = float64(int16(binary.LittleEndian.Uint16(p)))
			case 24:
				x := int32(uint32(p[0]) | uint32(p[1])<<8 | uint32(p[2])<<16)
				v = float64(x << 8 >> 8)
			case 32:
				v = float64(int32(binary.LittleEndian.Uint32(p)))
			}
			s += v
		}
		out[i] = s / float64(ch) / full
	}
	return out
}

// resample changes the rate by linear interpolation. When it lowers the rate,
// a moving average over the ratio first removes most content above the new
// Nyquist frequency (a simple low-pass; enough for speech recognition).
func resample(in []float64, from, to int) []float64 {
	if from == to || len(in) == 0 {
		return in
	}
	ratio := float64(from) / float64(to)
	src := in
	if ratio > 1 {
		w := int(math.Ceil(ratio))
		src = make([]float64, len(in))
		sum := 0.0
		for i := range in {
			sum += in[i]
			if i >= w {
				sum -= in[i-w]
			}
			k := w
			if i+1 < w {
				k = i + 1
			}
			src[i] = sum / float64(k)
		}
	}
	n := int(float64(len(in)) / ratio)
	out := make([]float64, n)
	for i := range out {
		x := float64(i) * ratio
		j := int(x)
		f := x - float64(j)
		a := src[j]
		b := a
		if j+1 < len(src) {
			b = src[j+1]
		}
		out[i] = a + (b-a)*f
	}
	return out
}

// memWS is an in-memory io.WriteSeeker (the WAV encoder seeks back to write
// the header sizes).
type memWS struct {
	buf []byte
	pos int
}

func (m *memWS) Write(p []byte) (int, error) {
	if need := m.pos + len(p); need > len(m.buf) {
		m.buf = append(m.buf, make([]byte, need-len(m.buf))...)
	}
	copy(m.buf[m.pos:], p)
	m.pos += len(p)
	return len(p), nil
}

func (m *memWS) Seek(off int64, whence int) (int64, error) {
	var p int64
	switch whence {
	case io.SeekStart:
		p = off
	case io.SeekCurrent:
		p = int64(m.pos) + off
	case io.SeekEnd:
		p = int64(len(m.buf)) + off
	}
	if p < 0 {
		return 0, errors.New("seek before start")
	}
	m.pos = int(p)
	return p, nil
}
