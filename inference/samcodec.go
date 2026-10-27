package inference

// The SAM one_bit mask codec, ported from Meta's reference parser
// meta-sam-parser 0.1.0 (_mask_codec.py; SAM License, see
// third_party/SAM_LICENSE.txt). The wire payload is "!" + base85 text with a
// 5-digit length prefix; the bytes are an adaptive binary range code with a
// 12-pixel causal context. Decoding re-encodes and compares, exactly as the
// reference does, so a non-canonical payload is refused rather than guessed.
// The "~" lossless encoding is not supported here: inference always asks
// for one_bit (metadata.mask_encoding).

import (
	"errors"
	"strings"
)

var errMask = errors.New("sam: invalid mask payload")

const (
	b85Radix     = 85
	b85Prefix    = 5
	rcTop        = 1 << 24
	rcBottom     = 1 << 16
	ctxCount     = 1 << 12
	ctxPadding   = 2
	cntIncrement = 14
	cntLimit     = 4096
)

var (
	b85Alphabet string
	b85Value    [128]int
)

func init() {
	var sb strings.Builder
	for c := 0x21; c < 0x7F; c++ {
		if !strings.ContainsRune("\"\\,;<>|", rune(c)) {
			sb.WriteByte(byte(c))
		}
	}
	b85Alphabet = sb.String()
	for i := range b85Value {
		b85Value[i] = -1
	}
	for i := 0; i < len(b85Alphabet); i++ {
		b85Value[b85Alphabet[i]] = i
	}
}

func b85Digit(c byte) (uint64, error) {
	if c >= 128 || b85Value[c] < 0 || b85Value[c] >= b85Radix {
		return 0, errMask
	}
	return uint64(b85Value[c]), nil
}

func b85Pack(in []byte) (string, error) {
	n := len(in)
	prefix := make([]byte, b85Prefix)
	rem := n
	for i := b85Prefix - 1; i >= 0; i-- {
		prefix[i] = b85Alphabet[rem%b85Radix]
		rem /= b85Radix
	}
	if rem != 0 {
		return "", errMask
	}
	out := append([]byte{}, prefix...)
	digits := func(v uint64) [5]byte {
		var d [5]byte
		for i := 4; i >= 0; i-- {
			d[i] = byte(v % b85Radix)
			v /= b85Radix
		}
		return d
	}
	off, full := 0, n-n%4
	for ; off < full; off += 4 {
		v := uint64(in[off])<<24 | uint64(in[off+1])<<16 | uint64(in[off+2])<<8 | uint64(in[off+3])
		for _, d := range digits(v) {
			out = append(out, b85Alphabet[d])
		}
	}
	if r := n - off; r > 0 {
		var v uint64
		for i := 0; i < 4; i++ {
			v <<= 8
			if i < r {
				v |= uint64(in[off+i])
			}
		}
		d := digits(v & 0xFFFFFFFF)
		for i := 0; i < r+1; i++ {
			out = append(out, b85Alphabet[d[i]])
		}
	}
	return string(out), nil
}

func b85Unpack(p string) ([]byte, error) {
	if len(p) < b85Prefix {
		return nil, errMask
	}
	var n uint64
	for i := 0; i < b85Prefix; i++ {
		d, err := b85Digit(p[i])
		if err != nil {
			return nil, err
		}
		n = n*b85Radix + d
	}
	r := int(n % 4)
	want := b85Prefix + int(n/4)*5
	if r != 0 {
		want += r + 1
	}
	if len(p) != want || n > 1<<30 {
		return nil, errMask
	}
	out := make([]byte, n)
	src, dst, full := b85Prefix, 0, int(n)-r
	for dst < full {
		var v uint64
		for i := 0; i < 5; i++ {
			d, err := b85Digit(p[src+i])
			if err != nil {
				return nil, err
			}
			v = v*b85Radix + d
		}
		if v > 0xFFFFFFFF {
			return nil, errMask
		}
		src += 5
		out[dst], out[dst+1], out[dst+2], out[dst+3] = byte(v>>24), byte(v>>16), byte(v>>8), byte(v)
		dst += 4
	}
	if r > 0 {
		var v uint64
		for i := 0; i < 5; i++ {
			d := uint64(b85Radix - 1)
			if i < r+1 {
				var err error
				if d, err = b85Digit(p[src+i]); err != nil {
					return nil, err
				}
			}
			v = v*b85Radix + d
		}
		if v > 0xFFFFFFFF {
			return nil, errMask
		}
		for i := 0; i < r; i++ {
			out[dst+i] = byte(v >> (24 - 8*i))
		}
	}
	if back, err := b85Pack(out); err != nil || back != p {
		return nil, errMask
	}
	return out, nil
}

type rangeEncoder struct {
	low, rng uint32
	out      []byte
}

func (e *rangeEncoder) encode(cum, freq, total uint32) {
	scaled := e.rng / total
	e.low += scaled * cum
	e.rng = scaled * freq
	for {
		if e.low^(e.low+e.rng) < rcTop {
		} else if e.rng < rcBottom {
			e.rng = -e.low & (rcBottom - 1)
		} else {
			break
		}
		e.out = append(e.out, byte(e.low>>24))
		e.low <<= 8
		e.rng <<= 8
	}
}

func (e *rangeEncoder) finish() []byte {
	for i := 0; i < 4; i++ {
		e.out = append(e.out, byte(e.low>>24))
		e.low <<= 8
	}
	return e.out
}

type rangeDecoder struct {
	in                     []byte
	pos                    int
	low, rng, code, scaled uint32
}

func newRangeDecoder(in []byte) (*rangeDecoder, error) {
	if len(in) < 4 {
		return nil, errMask
	}
	d := &rangeDecoder{in: in, rng: 0xFFFFFFFF}
	for i := 0; i < 4; i++ {
		b, err := d.read()
		if err != nil {
			return nil, err
		}
		d.code = d.code<<8 | uint32(b)
	}
	return d, nil
}

func (d *rangeDecoder) read() (byte, error) {
	if d.pos >= len(d.in) {
		return 0, errMask
	}
	b := d.in[d.pos]
	d.pos++
	return b, nil
}

func (d *rangeDecoder) frequency(total uint32) (uint32, error) {
	d.scaled = d.rng / total
	if d.scaled == 0 {
		return 0, errMask
	}
	v := (d.code - d.low) / d.scaled
	if v >= total {
		return total - 1, nil
	}
	return v, nil
}

func (d *rangeDecoder) decode(cum, freq uint32) error {
	d.low += d.scaled * cum
	d.rng = d.scaled * freq
	for {
		if d.low^(d.low+d.rng) < rcTop {
		} else if d.rng < rcBottom {
			d.rng = -d.low & (rcBottom - 1)
		} else {
			break
		}
		b, err := d.read()
		if err != nil {
			return err
		}
		d.code = d.code<<8 | uint32(b)
		d.low <<= 8
		d.rng <<= 8
	}
	return nil
}

func contextAt(r []byte, pos, prior, earlier int) int {
	return int(r[pos-1]) | int(r[pos-2])<<1 | int(r[prior-2])<<2 | int(r[prior-1])<<3 |
		int(r[prior])<<4 | int(r[prior+1])<<5 | int(r[prior+2])<<6 | int(r[earlier-2])<<7 |
		int(r[earlier-1])<<8 | int(r[earlier])<<9 | int(r[earlier+1])<<10 | int(r[earlier+2])<<11
}

func updateCounts(c []uint16, off int, bit byte) {
	c[off+int(bit)] += cntIncrement
	if int(c[off])+int(c[off+1]) >= cntLimit {
		for _, k := range []int{off, off + 1} {
			if c[k] >>= 1; c[k] == 0 {
				c[k] = 1
			}
		}
	}
}

// encodeOneBit is the reference encoder; decodeOneBit uses it to refuse
// non-canonical payloads, and the tests use it for round trips.
func encodeOneBit(raster []byte, w, h int) (string, error) {
	e := &rangeEncoder{rng: 0xFFFFFFFF}
	counts := make([]uint16, ctxCount*2)
	for i := range counts {
		counts[i] = 1
	}
	pw := w + 2*ctxPadding
	padded := make([]byte, pw*(h+ctxPadding))
	for y := 0; y < h; y++ {
		row := (y+ctxPadding)*pw + ctxPadding
		for x := 0; x < w; x++ {
			pos := row + x
			off := contextAt(padded, pos, pos-pw, pos-2*pw) << 1
			zero, one := uint32(counts[off]), uint32(counts[off+1])
			bit := raster[y*w+x]
			switch bit {
			case 0:
				e.encode(0, zero, zero+one)
			case 1:
				e.encode(zero, one, zero+one)
			default:
				return "", errMask
			}
			padded[pos] = bit
			updateCounts(counts, off, bit)
		}
	}
	p, err := b85Pack(e.finish())
	if err != nil {
		return "", err
	}
	return "!" + p, nil
}

// DecodeOneBit returns the row-major 0/1 raster (w*h) of a "!" payload.
func DecodeOneBit(payload string, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 || w*h > 1<<26 || !strings.HasPrefix(payload, "!") {
		return nil, errMask
	}
	packed, err := b85Unpack(payload[1:])
	if err != nil {
		return nil, err
	}
	d, err := newRangeDecoder(packed)
	if err != nil {
		return nil, err
	}
	counts := make([]uint16, ctxCount*2)
	for i := range counts {
		counts[i] = 1
	}
	pw := w + 2*ctxPadding
	padded := make([]byte, pw*(h+ctxPadding))
	out := make([]byte, w*h)
	for y := 0; y < h; y++ {
		row := (y+ctxPadding)*pw + ctxPadding
		for x := 0; x < w; x++ {
			pos := row + x
			off := contextAt(padded, pos, pos-pw, pos-2*pw) << 1
			zero, one := uint32(counts[off]), uint32(counts[off+1])
			v, err := d.frequency(zero + one)
			if err != nil {
				return nil, err
			}
			var bit byte
			if v < zero {
				err = d.decode(0, zero)
			} else {
				bit = 1
				err = d.decode(zero, one)
			}
			if err != nil {
				return nil, err
			}
			padded[pos], out[y*w+x] = bit, bit
			updateCounts(counts, off, bit)
		}
	}
	if back, err := encodeOneBit(out, w, h); err != nil || back != payload {
		return nil, errMask
	}
	return out, nil
}
