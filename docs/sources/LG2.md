# LG2 — sources for each decision

Change: LG2 (branch `logo`, after LG1 #107), the mark on the Android adaptive icon layers is 48 dp (`mobile/src/theme.ts` (`logo.adaptiveMark` in place of `logo.adaptiveVisible`), `mobile/tools/brand/brand.go`, `mobile/tools/brand/brand_test.go`, `mobile/assets/android-icon-foreground.png`, `mobile/assets/android-icon-monochrome.png`, `mobile/docs/style_guide.md`; NEW_WORK.md, the number read at run time).

T9 (2026-11-02), on the question "the Android foreground mark is 46 % of the visible part, about 37 x 33 dp, smaller than Android's 48 dp guide: keep it or make it bigger?": "keep it 48 dp".

Own evidence: the icon tool's 4 Go tests pass with the new token; the 5 other icon files are byte-equal to LG1 (the tool made them again from the same tokens); mobile tests 199 (unchanged).

### D363. The Android mark box is 48 of 108 dp
- https://developer.android.com/develop/ui/views/launch/icon_design_adaptive — "Size all layers to 108x108 dp"; "Use a logo that's at least 48x48 dp. It must not exceed 66x66 dp, because the inner 66x66 dp of the icon appears within the masked viewport." The mark box (130 x 118 path units) is now 48 dp high and 52.9 dp wide on the 108 dp foreground and monochrome layers (`logo.adaptiveMark` 48/108): at least 48 x 48 dp, at most 66 dp. A Go test fails when the box is under 48 dp or over 66 dp, or when a pixel of the mark is outside the 66 dp circle. The other icons (`icon.png`, `favicon.png`, `apple-touch-icon.png`, the background layer) do not change: the mark stays at `logo.iconShare` (46 %) there, as approved.
