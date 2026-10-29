package gate

// Goldens: sha256 of the bytes of Pillow 12.3.0 im.crop(centre square).resize((s, s), f)
// on pattern(w, h), computed 2026-10-29 (testdata/gold.py). Bit for bit.

var bilinearGoldens = []struct {
	w, h, s int
	sha     string
}{
	{53, 31, 16, "3f8afed84a6c0577f096547738ac21b8dd49f75ff4fb9ce1d7e78339028f36fa"},
	{31, 53, 20, "c5d8bafeff72464bcc96e785f61d5adfcec980853f5a975ab7a686228e5b8ca9"},
	{7, 9, 24, "af03f494c94a02c30aa67ca56fa534287e7d5ef4a114fe3e144adf5984d5ad14"},
	{1, 1, 5, "f915a3a12f64c3c02d8b1e0336c6cbf399865b04207c4b2e73d687eb389b3acc"},
	{800, 600, 384, "6fb21c30cc630bd6e51ff640eac22007d26ff286ea0097ae83bb8a053811ce01"},
	{300, 200, 192, "d055f0225a705ae2c28c6819cdfec05b56b51af3b05bde2f9c8fb2c957354b8d"},
	{13, 13, 13, "5096779f2a4438bcbf7310abd6dd168bf01609c7086189286f92fa72cbfb168c"},
}

var bicubicGoldens = []struct {
	w, h, s int
	sha     string
}{
	{53, 31, 16, "565c8a4f60ef57811a0caa7864c335a53313864dae132caf2a25482b8eb945ca"},
	{31, 53, 20, "cb942aac5954038f4fb37df93c6a8e468bfeb76f756bdcc7a23cc81c1dbcd2c7"},
	{7, 9, 24, "563a82b36a99ebfc33ccb6227fa5e75d87b213446899aba866a0a366eb160eda"},
	{1, 1, 5, "f915a3a12f64c3c02d8b1e0336c6cbf399865b04207c4b2e73d687eb389b3acc"},
	{800, 600, 384, "b8795be211ee88880f9c009bb73698fd3efbb4696cf0228fa415b4353afb8428"},
	{300, 200, 192, "be9d5a6b6da36ca199fe515c85dbe2b74af33fc6fdede31a0a21114d026956ac"},
	{13, 13, 13, "5096779f2a4438bcbf7310abd6dd168bf01609c7086189286f92fa72cbfb168c"},
}

// pattern is the test photo of gold.py: edges and gradients in packed RGB.
func pattern(w, h int) []uint8 {
	px := make([]uint8, 0, w*h*3)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px = append(px, uint8((x*37+y*11)%256), uint8((x*x+3*y)%256), uint8(((x^y)*29+7)%256))
		}
	}
	return px
}

// peTensorGolden: sha256 of the little-endian float32 bytes of the PE tensor of
// pattern(53, 31) at 16 px: Pillow BICUBIC, then (x/255 - 0.5)/0.5 in numpy
// float32, NHWC (testdata/gold2.py, Pillow 12.3.0, numpy 2.5.3, 2026-10-29).
const peTensorGolden = "4aec08320a994dcc155b2d082abe77ccaeb14387b3e7a9e726be56900096d410"

// nonSquareGoldens: Pillow resize((ow, oh), f) of pattern(w, h) with no crop
// (one side the same, or both different; testdata/gold2.py).
var nonSquareGoldens = []struct {
	w, h, ow, oh int
	filter, sha  string
}{
	{40, 30, 40, 12, "bilinear", "3ca2dccae3fd181753a7d130e7be1ec60115861be5cfb4f72638f485b6c55e50"},
	{40, 30, 17, 30, "bicubic", "f9b27424ae68226dc269f0a58ec7df9a876c20f953e00a5cbc989d23aa9d22ee"},
	{61, 23, 9, 31, "bicubic", "e230071a262d75c61c58367ae76d901776f60c15c5c3f6cd2b24e98c36796506"},
	{61, 23, 100, 7, "bilinear", "d31edb3c576d83e13d8ff6ffa69ae27c2463f762b6ec3167cc80c0c19d8b2257"},
}
