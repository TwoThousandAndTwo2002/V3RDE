# V3RDE style guide

Written new for V3RDE during the Hackathon (NEW_WORK_UI.md, UI 5). It is the rule book of the app UI.

**The rule.** Every screen and part takes its colours, sizes, radii, fonts, spacing, opacity and motion from `mobile/src/theme.ts`, and its controls from `mobile/src/ui.tsx`. A screen that needs a value that is not here stops: the value is proposed to T9, and after a yes it becomes a token here and in `theme.ts` in the same change. `mobile/tests/style.test.mts` fails a change that:
- puts a colour (hex, `rgb()`, `hsl()` or a colour name), a font, a text size, a corner radius that is not a `radius` token, or a fractional number in a part or a screen;
- adds a token to `theme.ts` that this guide does not list with the same value, or lists a token here that `theme.ts` does not have;
- makes a text colour pair less than 4.5:1 in light or dark mode.

**Where the values come from** (the "Source" column).
- **X32** — the look T9 approved on Oct 27 2026 (the X32 mockup, https://claude.ai/artifact/6AMxFSoWfFfGMySXPefUKW). **X25** — the scan mockup of Oct 25 2026 (the camera and the small sheets), the reference for the states X32 does not show. `mobile/tools/ui/measure_x32.py` reads the shots (phone 3 px = 1 pt, desktop 2 px = 1 pt) along fixed scan lines; the output is `mobile/tools/ui/x32_measure.json`. Text sizes come from glyph heights divided by the glyph heights in the Poppins font file. An independent check compared the new shots with all 25 reference shots.
- **Ref-login** — the sign-in screen of the earlier repository (Icarus `457a62c`, from before the kickoff). Its code is not carried: a harness renders it once, as a reference to measure, the same way as X25.
- **Brand** (pre-existing, labelled) — Hungree red `#BC202C` and the Poppins font (SIL Open Font Licence) are Hungree LLC brand assets from before the Hackathon.
- **Platform** — touch size (Apple Human Interface Guidelines: 44 × 44 pt, https://developer.apple.com/design/human-interface-guidelines/accessibility), text contrast (WCAG 2.1: 4.5:1, https://www.w3.org/TR/WCAG21/#contrast-minimum), window size classes (Material 3, https://m3.material.io/foundations/layout/applying-layout/window-size-classes).
- **Choice** — a value that the mockups do not show (dark mode, press feedback, motion). T9 can change it.

## Colour (`palette`)

| Token | Light | Dark | Source | Use |
|---|---|---|---|---|
| `bg` | `#F6F5F5` | `#141414` | X32 page | page behind the screens |
| `card` | `#FFFFFF` | `#1E1E20` | X32 | cards, fields, the secondary pill |
| `fg` | `#232323` | `#F3F3F3` | X32 | text and glyphs |
| `muted` | `#6D6D78` | `#A1A1AA` | X32 | labels and secondary text |
| `placeholder` | `#9F9DA4` | `#6D6D78` | X32; dark: Ref-login (T9, Oct 29) | the example text in an empty field (see the note) |
| `line` | `#DDDDDD` | `#3A3A3F` | X32 | 1 pt borders of cards, fields and the secondary pill |
| `accent` | `#BC202C` | `#BC202C` | Brand | the primary pill, the progress bar |
| `onAccent` | `#FFFFFF` | `#FFFFFF` | X32 | text on the accent |
| `accentSoft` | `#FFD4D7` | `#4A1D22` | X25 | an empty photo frame; the progress bar track |
| `danger` | `#C43140` | `#FF727D` | X32, X25; dark: Ref-login (T9, Oct 29) | error text |
| `iconFill` | `#F3F3F3` | `#2A2A2E` | X32 | the disc of an icon button |
| `handle` | `#DCDEDD` | `#4A4A50` | X32 | the grab bar of a sheet |
| `sheetTint` | `rgba(255,255,255,0.55)` | `rgba(20,20,22,0.62)` | X32, X25: a white tint of 0.55 fits all 23 states (light) | the frosted layer of a sheet or dialog, over the blur |
| `blurTint` | `light` | `dark` | Choice | the expo-blur tint under the frosted layer (a tint name, not a colour) |
| `shadeTop` | `rgba(0,0,0,0)` | `rgba(0,0,0,0)` | X32, X25 | the shade over the photo behind a sheet or dialog, at the top |
| `shadeMid` | `rgba(0,0,0,0.15)` | `rgba(0,0,0,0.15)` | X32, X25 | the shade at `shade.mid` of the height |
| `shadeEnd` | `rgba(0,0,0,0.7)` | `rgba(0,0,0,0.7)` | X32, X25 | the shade at the bottom |
| `cameraBg` | `#000000` | `#000000` | Choice | behind the live camera picture (it shows only before the picture starts) |
| `cameraBtn` | `rgba(0,0,0,0.45)` | `rgba(0,0,0,0.45)` | X25 | the buttons and the hint on the live camera |
| `cameraFg` | `#FFFFFF` | `#FFFFFF` | X25 | their glyphs and text, and the shutter |
| `scrimDim` | `rgba(0,0,0,0.43)` | `rgba(0,0,0,0.6)` | X25 (light) | behind the camera dialog on a wide screen (`#8C8C8C` over `bg`) |
| `doodle` | `#BC202C` | `#FF727D` | Ref-login | the food doodles (at `doodle.opacity`) |
| `panel` | `rgba(255,255,255,0.97)` | `rgba(30,30,32,0.97)` | the earlier app (0.94), made almost solid (U12 check: busy photos showed through) | the text panel on the photo of a listing card |
| `link` | `#BC202C` | `#FF727D` | Ref-login (light); dark: 6.98:1 on `bg` (the old `#BC202C` is 2.97:1 on `bg`, 2.68:1 on `card`; T9, Oct 29) | text buttons and links (Show, Hide, Create one) |

- Dark values: the dark colours of the X32 review page, of Ref-login, or a choice. The accent stays Hungree red in dark mode (white on it is 6.21:1). Every text colour reaches 4.5:1 on `card` and on `bg`.
- Light `danger` stays `#C43140`: the Ref-login `#DC3545` is only 4.16:1 on `bg` `#F6F5F5` (T9, Oct 29).
- Every text pair reaches 4.5:1 in both modes (the style test computes it): `fg`, `muted`, `danger` and `link` on `card` and `bg`, `onAccent` on `accent`, `fg` on `iconFill`.
- **Note:** `placeholder` on `card` is 2.68:1. It is the approved X32 look. WCAG 2.1 does not exempt placeholder text, so a field must always have a label (`Field label=…`) that tells what to type; the placeholder only gives an example.
- **Limit:** text on the photo (the camera hint) and on the frosted layer has a background that changes with the photo, so the test cannot compute its contrast. Measured in the shots: muted and error text on the light frost is 2.6–3.9:1 in the approved X32/X25 shots (the same in the new shots); on the dark frost the lowest is 4.31:1 (muted) and 4.41:1 (error).

## Type (`type`, `font`)

| Token | Value | Source | Use |
|---|---|---|---|
| `font.regular` | Poppins_400Regular | Brand | body text |
| `font.semibold` | Poppins_600SemiBold | Brand | titles, pill text |
| `type.title` | 18 / 24 | X32: 17.8 pt ("6 items found"), 18.1 pt ("Your listing") | sheet titles (SemiBold) |
| `type.body` | 14 / 20 | X32: 14.0 pt, line 20.3 pt (muted text, 2 lines) | fields, labels (`muted`), notes, errors (`danger`) (Regular) |
| `type.button` | 14 / 20 | X32: 14.0 pt ("Start over"), 13.8 pt ("Post 6 listings") | pill text (SemiBold) |

Values are font size / line height in pt.

## Spacing (`space`, pt)

| Token | Value | Source | Use |
|---|---|---|---|
| `space.hair` | 4 | X32 | a title to its muted line; a field label to its field |
| `space.inset` | 6 | X32: 6.0; Ref-login | card padding; the indent of a field label and of a note; a note under its field; above the photo row of a dialog; the sign-in title to its line |
| `space.gap` | 8 | X32: 8.0 | between the rows and controls inside a card; above an error line; the end of the scroll body of a sheet |
| `space.item` | 10 | X32: 10.0 | between cards; between two pills (9.7 phone, 10.0 desktop); a thumbnail to its fields; above a field label |
| `space.group` | 12 | X32 | above a stand-alone label ("Servings" in the one-item form) |
| `space.gutter` | 16 | X32: 16.0 | screen edge to content on a phone; a dialog to the window edge (at least); the scroll body of a sheet to its pills; the line of a small sheet to its pills; the camera dialog padding |
| `space.foot` | 20 | X32: 20.0 | the last pill to the bottom of a phone sheet; the photo to the list in a dialog; a small sheet title to its pills (when there is no line) |
| `space.pad` | 24 | X32: 24.0 | dialog padding |
| `space.bar` | 28 | X25: the bar at 716, the title box ends at 688 | a sheet title to the progress bar under it |
| `space.grid` | 14 | the earlier app | between the listing cards of a screen (both ways) |

## Radius (`radius`, pt)

| Token | Value | Source | Use |
|---|---|---|---|
| `radius.photo` | 12 | X32: 11.3–11.7 (card 18 − inset 6: concentric) | a photo inside a card |
| `radius.card` | 18 | X32: 17.7 | cards, a field with several lines, the big photo of a dialog, the photo of the camera dialog |
| `radius.sheet` | 26 | X32, X25: 26.0 (circle fit to the corner) | the top corners of a phone sheet; the corners of a dialog and of the camera dialog |
| `radius.round` | 999 | X32 | pills, fields, icon buttons, the grab bar, the progress bar, the shutter, the camera hint (fully round) |

## Size (`size`, pt)

| Token | Value | Source | Use |
|---|---|---|---|
| `size.border` | 1 | X32: 1.0 | borders of cards, fields and pills |
| `size.control` | 48 | X32: 48.0 | field and pill height |
| `size.controlTall` | 72 | X32: 70 inside + 2 borders | a field with several lines (the pickup field) |
| `size.fieldPadX` | 18 | X32: text 19.3 pt from the field edge | field text to the field border (sides) |
| `size.fieldPadTop` | 12 | X32: text top 697.3 in a field at 680 | field text to the top border in a field with several lines |
| `size.pillPadX` | 20 | Choice | pill text to the pill border, when the pill does not grow |
| `size.iconSmall` | 38 | X32: 38.0 | icon button inside a review card (its touch area grows to 44) |
| `size.iconBig` | 44 | X32: 44.0; Platform | icon button of the one-item form |
| `size.thumb` | 94 | X32: 94.0 × 94.0 (T9, Oct 27: 92–94) | the item photo of a review card |
| `size.handleW` | 40 | X32: 40.0 | the grab bar width |
| `size.handleH` | 4 | X32: 4.0 | the grab bar height |
| `size.handleTop` | 10 | X32 | sheet top to the grab bar |
| `size.sheetLarge` | 0.85 | X32: the sheet starts at 127 of 844 pt | a large phone sheet is as tall as its content, but at most this share of the screen height (rounded to a whole pt) |
| `size.sheetSmall` | 210 | X25: the small sheet starts at 634 of 844 pt | a small phone sheet is at least this tall; its content starts at the top |
| `size.sheetMedium` | 0.44 | the earlier app (the role sheet) | a medium phone sheet (the role sheet): this share of the screen height |
| `size.dialogWide` | 860 | X32: 860 | a dialog with the photo on the left |
| `size.dialogExtraWide` | 1140 | X49 (T9, Oct 30: option A, the details column) | the review dialog with the details column at the right (`Sheet` `extraWide`) |
| `size.detailsColumn` | 260 | X49 (T9, Oct 30) | the details column of the wide review |
| `size.scanPhoto` | 3 / 4 | X49 (T9, Oct 30: option A) | the scan photo of the wide review (width to height); it is as high as the column |
| `size.dialogSmall` | 440 | X32: 440 | a dialog with a title, a line and two pills |
| `size.dialogCamera` | 640 | X25: 640 | the camera dialog on a wide screen (a 4:3 photo, 608 pt wide) |
| `size.dialogMax` | 0.869 | X32: 782 of 900 pt | a dialog is at most this share of the window height |
| `size.blur` | 40 | X32, X25 (frost check) | expo-blur intensity of the frosted layer |
| `size.progressH` | 6 | X25: 6.0 | the progress bar height |
| `size.focusBorder` | 2 | Ref-login: 2.00 | the border of a field while it has the focus (in `accent`) |
| `size.fieldSlot` | 76 | Ref-login | field text to the right border when a text button (Show) sits in the field |
| `size.loginTop` | 0.14 | Ref-login | the sign-in title from the top of a phone screen, as a share of the screen width (rounded to a whole pt) |
| `size.header` | 56 | the earlier app | the ☰ header of a phone or tablet screen |
| `size.topBar` | 64 | the earlier app | the top bar of a wide screen (the brand, the role tabs, Log out) |
| `size.tabPadX` | 16 | the earlier app | a role tab and the Log out button of the top bar: text to the edge (sides) |
| `size.toastBottomWide` | 28 | the earlier app | the same on a wide screen (no bottom pill there) |
| `size.toastBottom` | 88 | the earlier app (28), raised (U15 check: the toast covered the "+ New listing" pill) | a toast: its bottom to the screen bottom (over the bottom pill of a phone screen) |
| `size.feedPhoto` | 290 | the earlier app | the photo of a listing card on Receive |
| `size.minePhoto` | 220 | the earlier app | the photo of a listing card on Give (Your listings) |
| `size.timeW` | 88 | the earlier app | the time pill of a listing card (one width for every time) |
| `size.timePadY` | 2 | the earlier app | its padding (top, bottom) |
| `size.loadAhead` | 320 | the earlier app | a list loads its next page when it is this near its end (pt) |
| `size.choiceThumb` | 64 | a choice | a photo to choose on the review (the original or the touched-up photo) |
| `size.proposalMax` | 140 | a choice (U17 check: the box took 40 % of a phone sheet) | the lines of a voice proposal scroll above this height |

## Glyphs (`glyph`)

The glyphs are SVG paths with round ends (react-native-svg, MIT, https://github.com/software-mansion/react-native-svg), drawn in a square box in pt. Each path ends half a stroke inside its box, so the round ends touch the box edges.

| Token | Value | Source | Use |
|---|---|---|---|
| `glyph.box` | 12 | X32: ✕ and + 11.5–11.7 (ink), − 12.0 wide | the box of close, minus and plus |
| `glyph.uploadBox` | 15 | X25: 15.0 × 15.1 (ink) | the box of upload |
| `glyph.iconBox` | 20 | a choice (the earlier app's icon size) | the box of give, receive and menu |
| `glyph.stroke` | 1.67 | X32: − 1.68 pt (phone), 1.65 pt (desktop) | the line width |
| `glyph.close` | M0.835 0.835 L11.165 11.165 M11.165 0.835 L0.835 11.165 | X32 | ✕ (remove an item; close the camera) |
| `glyph.minus` | M0.835 6 H11.165 | X32 | − (fewer servings) |
| `glyph.plus` | M0.835 6 H11.165 M6 0.835 V11.165 | X32 | + (more servings) |
| `glyph.upload` | M7.5 0.835 V10.8 M4.25 4 L7.5 0.835 L10.75 4 M0.835 11 V12.2 A2.2 2.2 0 0 0 3.035 14.165 H11.965 A2.2 2.2 0 0 0 14.165 12.2 V11 | X25 | upload: an arrow up over an open tray with round bottom corners |
| `glyph.give` | M2.5 6.5 H6 L7.5 4 H12.5 L14 6.5 H17.5 A1.5 1.5 0 0 1 19 8 V15.5 A1.5 1.5 0 0 1 17.5 17 H2.5 A1.5 1.5 0 0 1 1 15.5 V8 A1.5 1.5 0 0 1 2.5 6.5 Z M10 9 A3 3 0 1 0 10 15 A3 3 0 1 0 10 9 Z | a choice (drawn new) | a camera: the Give card of the role sheet |
| `glyph.receive` | M1.5 9.5 H18.5 A8.5 6.5 0 0 1 1.5 9.5 Z M7 18.5 H13 M7.5 6.5 C7.5 5.2 8.5 5 8.5 3.5 M11.5 6.5 C11.5 5.2 12.5 5 12.5 3.5 | a choice (drawn new) | a bowl with steam: the Receive card |
| `glyph.menu` | M3 5.5 H17 M3 10 H17 M3 14.5 H17 | a choice (drawn new) | ☰: opens the role sheet from a screen |
| `glyph.mic` | M10 1.5 A3 3 0 0 1 13 4.5 V10 A3 3 0 0 1 7 10 V4.5 A3 3 0 0 1 10 1.5 Z M4 9.5 A6 6 0 0 0 16 9.5 M10 15.5 V18.5 M7 18.5 H13 | a choice (drawn new) | the microphone: talk to change the review |
| `glyph.stop` | M6.5 5 H13.5 A1.5 1.5 0 0 1 15 6.5 V13.5 A1.5 1.5 0 0 1 13.5 15 H6.5 A1.5 1.5 0 0 1 5 13.5 V6.5 A1.5 1.5 0 0 1 6.5 5 Z | a choice (drawn new) | stop talking |
| `glyph.sparkle` | M10 2 L11.6 8.4 L18 10 L11.6 11.6 L10 18 L8.4 11.6 L2 10 L8.4 8.4 Z | X49 (T9, Oct 30) | the Touch up pill |
| `glyph.info` | M10 1.5 A8.5 8.5 0 1 0 10 18.5 A8.5 8.5 0 1 0 10 1.5 Z M10 9 V14 M10 6 V6.2 | X49 (T9, Oct 30) | the Details pill |
| `glyph.pencil` | M13.5 3 L17 6.5 L7 16.5 H3.5 V13 Z | X49 (T9, Oct 30) | Change (the details column) |

## Shade (`shade`)

| Token | Value | Source | Use |
|---|---|---|---|
| `shade.mid` | 0.4 | X32, X25: alpha 0.13 at y 300 and 0.44 at y 610 (phone); 0.07, 0.24 and 0.64 at y 180, 450 and 840 (desktop) | the stop of `shadeMid`, as a share of the height (`shadeTop` at 0, `shadeEnd` at 1) |

## Camera (`camera`, pt)

| Token | Value | Source | Use |
|---|---|---|---|
| `camera.edge` | 14 | X25 | the close button from the top-left corner |
| `camera.side` | 32 | X25 | the upload button from the left edge |
| `camera.uploadBottom` | 48 | X25: the button centre at 777.0 | the upload button bottom to the screen bottom |
| `camera.shutter` | 76 | X25: 76 | the shutter: outer diameter |
| `camera.shutterRing` | 5 | X25: 5 | its white ring |
| `camera.shutterGap` | 4 | X25: 4 | the gap from the ring to the white disc (58) |
| `camera.shutterBottom` | 32 | X25: 812 of 844 | the shutter bottom to the screen bottom |
| `camera.cardShutterBottom` | 16 | X25 | the shutter bottom to the photo bottom in the camera dialog |
| `camera.hintBottom` | 124 | X25 | the hint pill bottom to the screen bottom |
| `camera.hintPadX` | 16 | X25 | the hint pill padding (sides) |
| `camera.hintPadY` | 10 | X25 | the hint pill padding (top and bottom) |
| `camera.ratio` | 4 / 3 | X25: 608 × 456 | the photo in the camera dialog (width to height) |

## Opacity and motion (`opacity`, `motion`)

| Token | Value | Source | Use |
|---|---|---|---|
| `opacity.pressed` | 0.85 | Choice | a control while a finger is on it |
| `opacity.busy` | 0.6 | X32: the Post pill while posting (`#C05D60` on the frost) | a control while it waits (the spinner shows) |
| `opacity.hover` | 0.92 | Ref-login | a pill while the mouse is on it (web) |
| `opacity.gone` | 0.6 | the earlier app | a listing card that is collected or out of time |
| `motion.rowStepMs` | 40 | Choice | each row of a sheet starts 40 ms after the row before it |
| `motion.rowRise` | 8 | Choice | a row fades in and rises this far (pt) |
| `motion.rowMs` | 220 | Choice | how long a row takes (ms) |
| `motion.sheetSpeed` | 14 | Choice | the spring of a sheet (React Native `Animated.spring` speed) |
| `motion.sheetBounce` | 0 | Choice | its bounciness (no overshoot) |
| `motion.sheetShift` | 0.2 | Choice | a sheet starts this share of the window height below its place |
| `motion.handoffMs` | 300 | Ref-login | wide screens: the sign-in card fades out in this time after sign-in |
| `motion.handoffLift` | 40 | Ref-login | and rises this far (pt) |
| `motion.dimMs` | 250 | the earlier app | the dim layer behind a sheet over a screen fades in in this time |
| `motion.growMs` | 220 | X49 (T9, Oct 30) | the details box opens in this time; then its text types in |
| `motion.growFrom` | 0.96 | a choice | the size the details box opens from (a share of its size) |
| `motion.letterMs` | 14 | X49 (T9, Oct 30) | the time of one letter of the text that types in |
| `motion.lineMs` | 220 | X49 (T9, Oct 30) | the pause at the end of a line of the text that types in |

## Doodles (`doodle`)

New line drawings of food (20, `mobile/src/doodles.tsx`) as a faint wallpaper, for the sign-in screen and later screens. There is a drawing in every cell of a grid. The cellular automaton rule 30 (`mobile/src/automata.ts`, from a random first row) makes each cell live or dead: a drawing in a dead cell has `doodle.faint` of the strength. Every `doodle.stepMs` a new generation comes in at the bottom and the pattern moves up one row; the drawings stay in their cells (T9, Oct 29, from an animated sample: 1000 ms, faint 55 %, wallpaper 9 %, rows move up). With "reduce motion" on, the pattern stands still. A hash picks the drawing, its turn and its shift; no drawing is the same as the one to its left or above it. The start is the same each time (a fixed seed); a change of the column count (a new width) gives a new pattern.

| Token | Value | Source | Use |
|---|---|---|---|
| `doodle.opacity` | 0.09 | Ref-login | light mode |
| `doodle.opacityDark` | 0.1 | Ref-login | dark mode |
| `doodle.units` | 24 | Choice | the box of the drawing paths (path units) |
| `doodle.glyph` | 32 | Ref-login: the earlier drawings measure 22–32 pt (the new ones 14–28 pt in this box) | the box of one drawing (pt) |
| `doodle.stroke` | 2.2 | Ref-login | the line width of a drawing (pt) |
| `doodle.cell` | 38 | Ref-login: the ink per area | the grid cell (pt), one drawing in each |
| `doodle.jitter` | 3 | Choice | the largest shift of a drawing from the centre of its cell (pt) |
| `doodle.turn` | 20 | Choice | the largest turn of a drawing (degrees) |
| `doodle.turnSteps` | 2 | Choice | turn steps on each side of 0 (-20, -10, 0, 10, 20) |
| `doodle.seed` | 1026 | Choice | the start of the automaton |
| `doodle.rowDensity` | 0.5 | Choice | the share of live cells in the random first row of rule 30 |
| `doodle.faint` | 0.55 | T9, Oct 29 (animated sample) | the strength of a drawing in a dead cell (a live cell has 1) |
| `doodle.stepMs` | 1000 | T9, Oct 29 (animated sample) | the time of one generation: the pattern moves up one row |

## Layout (`layoutFor(width)`)

| Function | Width (pt) | Kind | Popups | Columns | Header | Max width | Source |
|---|---|---|---|---|---|---|---|
| `layoutFor` | below 600 | compact | bottom sheets | 1 | ☰ header | 560 | Platform (Material 3 window size classes); columns and widths: the earlier app |
| `layoutFor` | 600 to 839 | medium | centred dialogs | 2 | ☰ header | 900 | Platform; the earlier app |
| `layoutFor` | 840 and more | expanded | centred dialogs | 3 | top bar with role tabs | 1120 | Platform; the earlier app |

## Parts (`mobile/src/ui.tsx`)

- **Text:** `Title` (`type.title`, `fg`), `Body` (`type.body`, `fg`), `Muted` (`type.body`, `muted`), `ErrorText` (`type.body`, `danger`, `space.gap` above it, read out as an alert).
- **GlyphIcon:** a glyph from `glyph` in its box, `glyph.stroke` wide, round ends.
- **IconBtn:** a round `iconFill` disc with a glyph in `fg`: `size.iconBig`, or `size.iconSmall` inside a review card (its touch area grows to `size.iconBig`). `opacity.pressed` while pressed.
- **Pill:** `size.control` high, fully round, `size.border`. `primary` = `accent` fill with `onAccent` text; `secondary` = `card` fill, `line` border, `fg` text. With `grow`, two pills share a row, `space.item` apart: secondary on the left, primary on the right. While busy: a spinner in place of the text, and `opacity.busy`; a tap does nothing, and the pill keeps the keyboard focus. With the mouse on it (web): `opacity.hover`.
- **FieldButton:** a text button inside a field (Show, Hide): `type.button` in `link`, `space.item` at the sides and `space.group` above and below (a 44 pt touch area, Platform), `space.gap` from the right border; it has its own name for a screen reader ("Show password").
- **FrostBtn (X49h):** a round frosted button on a photo: `size.iconSmall` (its touch area grows to `size.iconBig`), the frosted layer of the sheets (`size.blur`, `blurTint`, `sheetTint`, the corners `radius.round` on every layer), the glyph in `fg`. Delete on a listing card: `close`, at the top right of the photo, `space.inset` from the edges.
- **SmallPill (X49h):** a primary pill the size of the time pill: `size.timeW` wide, `size.timePadY` top and bottom, `radius.round`, `type.button` in `onAccent` on `accent`; its touch area grows by `space.group`. Rescue on an expired card.
- **TextLink:** an action in a line of text ("Create one"): `type.button` in `link`, underlined; a screen reader reads it as a button. R5: with `touch`, `space.group` above and below (a 44 pt touch area, Platform), for a link that stands alone in a row (the links of a rescue place).
- **ChipBtn:** a small pill, `size.iconSmall` high (its touch area grows to `size.iconBig`), `radius.round`, `iconFill` fill, `size.border`: a glyph and a short word (`type.button`, `space.inset` apart, `space.group` at the sides). `on` (the part it opens is open, or the chosen choice) = `accent` border, `link` text and glyph (`link` is `accent` in light mode; it stays readable in dark mode). A choice chip (no glyph) has a `line` border when it is off, and a screen reader reads it as a radio button.
- **Grow:** a part that opens fades in and grows from `motion.growFrom` in `motion.growMs` (at once with "reduce motion").
- **TypedText:** lines that type in, one letter each `motion.letterMs`, with `motion.lineMs` at the end of a line, after a delay; the first line `fg`, the next ones `muted`. With "reduce motion" on, the text shows at once. A screen reader reads all the text at once.
- **Doodles:** the doodle wallpaper; it fills its parent under the content and draws for the size of its parent; the rule-30 pattern moves up one row every `doodle.stepMs` (still with 'reduce motion'); no touches; hidden from screen readers.
- **Row:** controls in a line, `space.item` apart; `tight` = `space.gap` apart (inside a card).
- **Field:** `size.control` high, fully round, `card` fill, `size.border` `line` border, text `size.fieldPadX` from the sides, `placeholder` for the example text. With the focus: a `size.focusBorder` border in `accent` (no browser focus ring). With `right` (a FieldButton): text `size.fieldSlot` from the right border. The label is also the name of the input for a screen reader. With several lines: `size.controlTall`, `radius.card`, text `size.fieldPadTop` from the top. A label is `Muted`, `space.inset` in, `space.item` above it, `space.hair` to the field.
- **ReviewCard:** one found item: `card` fill, `size.border` `line`, `radius.card`, `space.inset` padding, `space.item` above it. The `size.thumb` photo (`radius.photo`), `space.item` to two `tight` rows `space.gap` apart: the title field with ✕ (remove), and − servings +, with `size.iconSmall` buttons. An error line goes under the rows.
- **Rise:** each row of a sheet fades in and rises `motion.rowRise`, `motion.rowStepMs` after the row before it, in `motion.rowMs`.
- **Sheet:** the photo behind it with the shade over it (`shadeTop`, `shadeMid` at `shade.mid`, `shadeEnd`), then a frosted layer (the blur of the photo, `size.blur`, `blurTint`, with `sheetTint` over it). Compact width: a bottom sheet, `radius.sheet` top corners, the grab bar (`size.handleW` × `size.handleH`, `handle`, `size.handleTop` from the top, `space.gutter` to the content), `space.gutter` sides, `space.foot` under the last pill. `large` = as tall as its content, at most `size.sheetLarge` of the screen height; `small` = at least `size.sheetSmall`. Medium and expanded width: a centred dialog, `radius.sheet` corners, `space.pad` padding, at most `size.dialogMax` of the window height; `wide` = `size.dialogWide` with the photo on the left (`space.foot` to the list), `extraWide` = `size.dialogExtraWide` (the review with the details column), else `size.dialogSmall`. It rises with a spring (`motion.sheetSpeed`, `motion.sheetBounce`, from `motion.sheetShift` of the window height).
- **SheetBody:** the scrolling body of a large sheet; it ends `space.gap` under its content.
- **SheetPills:** the pill row of a large sheet, `space.gutter` under the body (a line such as "Posting 3 of 6…" goes between them, `space.gap` under the body).
- **Progress:** `size.progressH` high, fully round, `accent` over `accentSoft`; `space.bar` under the sheet title.
- **Photo:** a square photo (or `side` × `side`) with a `radius` token; an empty frame shows `accentSoft`.
- **Camera:** the live picture on `cameraBg`; `CameraBtn` (a `size.iconSmall` disc in `cameraBtn` with a `cameraFg` glyph: close at `camera.edge`, upload at `camera.side` × `camera.uploadBottom`), `Hint` (a `cameraBtn` pill, `camera.hintPadX` × `camera.hintPadY`, `camera.hintBottom` from the bottom), `Shutter` (`camera.shutter`, `camera.shutterRing`, `camera.shutterGap`, `camera.shutterBottom`).
- **CardDialog:** the camera on a wide screen: a `card` dialog, `size.dialogCamera` wide, `space.gutter` padding, `radius.sheet`, on `scrimDim`. The photo is `camera.ratio` with `radius.card` and the shutter `camera.cardShutterBottom` from its bottom; the pills are `space.gutter` under the photo, and the hint `space.gap` under the pills.

## Sign-in (`mobile/src/screens/Login.tsx`, words in `mobile/src/signin.ts`)

- Phone: `Doodles` on `bg`; the title `size.loginTop` × the screen width from the top, `space.gutter` sides; the line `space.inset` under the title; the fields `space.pad` under the line; the rule line `space.inset` under the password field (`space.inset` in); the Continue pill `space.gutter` from the sides and the bottom.
- Wide screen: the same content in a `card` card, `size.dialogSmall` wide, `space.pad` padding, `radius.sheet`, `size.border` `line`, centred on the doodles; the pill `space.pad` under the content. After sign-in the card fades out in `motion.handoffMs` and rises `motion.handoffLift`; then the next screen comes.
- Words (T9, Oct 29: the earlier words, made simpler): "Share or find a meal", "Sign in or sign up", "8 characters or more", "Continue". A 401 on sign-in shows "No account found." with the link "Create one" (it signs up with the same email and password); other failures show the text of the services.
- One request at a time; Enter in the email field goes to the password field; Enter in the password field signs in and keeps the focus there.
- The email has no spaces at the ends and must have the form name@domain.end (the same check as the auth service), else "Enter an email like you@email.com". The password field has no auto-capital, auto-correct or spell check; after "No account found" it asks the browser for a new password.
- While it waits, a screen reader reads the pill as "Signing in…".
- "Forgot password?" (a TextLink, `space.gap` under the rule line and the error lines, `space.inset` in) opens the reset screen.
- The phone and wide-screen frame is `AuthFrame` (`mobile/src/screens/AuthFrame.tsx`), shared with the reset screens.

## Reset password (`mobile/src/screens/Reset.tsx`, words in `mobile/src/signin.ts`)

- Ask: "Reset your password", "We send a link to your email", the email field, the pill "Send link" (busy: "Sending…"), and the TextLink "Back to sign in" `space.gutter` under the field. Then "Check your email" with "If an account uses <email>, a link to set a new password is on its way. It works for 30 minutes." and the pill "Back to sign in". The same answer for every email.
- The link (web `/reset#token=…`, app `v3rde://reset#token=…`: after - The layout is `AuthFrame`, the same as sign-in.

## Role sheet (`mobile/src/screens/Role.tsx`, words in `mobile/src/words.ts`)

- A `Popup` (a sheet over the screen on view): `scrimDim` behind it (it fades in in `motion.dimMs`), then the frosted layer. Phone: a bottom sheet with the grab bar, `size.sheetMedium` of the screen height. Wide screen: a centred dialog, `size.dialogSmall` wide, `space.pad` padding, `radius.sheet`.
- "What do you want to do?" (`type.title`), then two role cards `space.gutter` under it and `space.item` apart, then the secondary pill "Log out" `space.gutter` under them. The rows rise one after the other (`Rise`).
- A role card: `card`, `size.border` `line` (`accent` when it is the role on view or under the mouse), `radius.card`, `space.group` padding and gap; a `size.iconBig` disc in `accentSoft` with the glyph in `accent`; the name (`type.button`) over a `muted` line. Give: a camera, "Share your food". Receive: a bowl, "Find food near you".
- When it shows: right after sign-in on a phone (over the sign-in screen, which stays under the dim); right after sign-in on a wide screen (the sign-in card fades out, then the role dialog rises over the doodles with no dim); when the app opens with a session (over the doodles, with the dim); from ☰ on a screen (then a tap on the dim, Esc or Back closes it).

## Frame (`mobile/src/screens/Frame.tsx`)

- Phone and tablet (`layoutFor` compact and medium): a header `size.header` tall, `space.gutter` at the sides: the ☰ icon button (`size.iconBig`, glyph menu, name "Menu") at the left opens the role sheet; "V3RDE" in `muted` in the centre.
- Wide screen (expanded): a top bar `size.topBar` tall with a `size.border` `line` under it, `space.pad` at the sides, its content at most `maxWidth` wide (each screen also centres its own content in `maxWidth`): "V3RDE" (`type.title`, `link`) at the left; the role tabs Give and Receive in the centre (`size.control` tall, `size.tabPadX` at the sides, `space.inset` apart; the chosen tab `link` text on `accentSoft`, the others `muted`, `iconFill` under the mouse); "Log out" at the right (`size.border` `line`, `muted` text). The role sheet is not used there after the first pick. Only the chosen tab is a Tab-key stop; Left and Right choose the other tab.
- The screens keep clear of the status bar, the home bar and a notch at the side (react-native-safe-area-context insets); the toast and the role sheet also add the bottom inset.
- A toast (`Toast`): `bg` text on `fg`, fully round, `camera.hintPadX` × `camera.hintPadY` padding, `size.toastBottom` above the home bar; a screen reader reads each new text (a polite live region; an announcement on iOS). "Your session has ended. Sign in again." shows when the services answer 401.

## Receive (`mobile/src/screens/Receive.tsx`, words in `mobile/src/words.ts`, rules in `mobile/src/listing.ts`)

- "Available now" (`type.title`) in a `ScreenHead` (a row `size.control` high, so the title is at the same level as on Give, U19) `space.gutter` over the cards; the content is at most `maxWidth` wide and centred, `space.gutter` at the sides (`space.pad` with the top bar, and `space.pad` above). The doodles are behind it.
- The cards in `columns` columns, `space.grid` apart. A listing card: the photo `size.feedPhoto` tall in `radius.card` (`accentSoft` while it loads); on it a `panel` panel, `space.inset` from the card edges, `radius.photo`, `space.group` padding, `space.hair` between lines: the title (`type.button`, at most 2 lines), the servings with the time pill at the right (`size.timeW`, m:ss in `onAccent` on `accent`; "Collected" or "Expired" in `muted` on `iconFill`), the pickup text (1 line), and the Grab pill `space.gap` under it. The user's own listings are not shown (U19; they are under Give). A card that is gone has `opacity.gone`.
- More listings load when the list is `size.loadAhead` from its end, and at once while the cards do not fill the screen; on a phone, pull down to load again. Empty: "Nothing is offered now. Look again in a few minutes."
- After Grab: a small `Sheet` over the listing photo: "You grabbed it", the title and the pickup text, and the pills "Maps" (a map search) and "WhatsApp" (a message to the giver). The ✕, a tap on the photo, Esc or Back closes it. A listing that someone took first (409) shows as collected, with a toast.
- X49e (T9, Oct 30, the approved "distance" mockup v2; rules in `mobile/src/place.ts`): the list is for a place. The first time, a small `Popup` (no dismiss while there is no place): "Find food near you", "Your place shows only the food within the distance you pick. Nobody else sees your place.", the pills "Type a ZIP" (secondary) and "Use my location" (busy while the browser finds it). If location is off or not there: the ZIP popup ("Your ZIP code", the field, "Back" and "Show food"; a bad ZIP: "Type a 5-digit ZIP code."). The choice and the distance stay on this device (web storage; nothing breaks when it is blocked).
- Under the title: the choice chips "1 mi", "5 mi", "10 mi" (`ChipBtn` with no glyph, 5 mi first), and under them "Near your location" or "Near ZIP 10001" in `muted` with the text link "Change", `space.gap` apart, `space.gutter` over the cards. The servings line adds the distance with one decimal: "6 servings · 0.4 mi".
- The list is nearest first, up to 25 mi (the server's limit). Food within the distance shows as before; food farther shows under "Farther than 1 mi" (`muted`, `space.gutter` above) in gray (the card at `opacity.gone`), still with Grab. A Grab of a gray card opens a small `Popup`: "This food is 3.4 mi away", "It is farther than your 1 mi. The timer ends in 12 min. Grab it only if you can get there in time.", "Cancel" and "Grab". Nothing within the distance: "Nothing within 1 mi now. Look again in a few minutes." (the gray part stays under it).
- After a grab the card goes to "Your pickup" at the top (also after a reload: GET /api/listings/grabbed), with the time left, Maps, WhatsApp and the text link "Release it" (until the timer ends). "More near you" is over the rest. Release opens a small `Popup`: "Release Roma tomatoes?", "Other people near it can grab it again, while its timer runs.", "Keep it" and "Release"; then the toast "Released. Other people can grab it now." (or "The timer ended. The grab is final.").

## Give (`mobile/src/screens/Give.tsx`)

- The list of each role scrolls inside the frame (U19: the frame is `flex: 1` with `minHeight: 0`, so it is never taller than the window).
- "Your listings" in the same `ScreenHead` as Receive (with "+ New listing" at its right on a wide screen) and the same layout as Receive; the listing card has the photo `size.minePhoto` tall and only the title, the servings and the time pill. Empty: "Nothing is listed yet. Take a photo of a meal to share it."
- X48t: a card nobody has grabbed has "Delete": X49h (T9, Oct 31) an X in a `FrostBtn` at the top right of the photo (before: a text link in its panel). A tap opens a small `Popup` (`small`: on a phone the small sheet, at least `size.sheetSmall`, as high as its content; wide: the small dialog): "Delete “…”?", "People will no longer see it. This cannot be undone.", the pills "Keep" (secondary) and "Delete". Then the toast "Deleted. People no longer see it." A grabbed card says "Someone is picking this up. It cannot be deleted." in `muted`.

- R5: a gone card keeps its actions at full strength: `opacity.gone` is on the photo and on the title and servings lines only, so a link or pill in the panel keeps 4.5:1 (independent check of the R5 mockup).

## Rescue (`mobile/src/screens/Rescue.tsx`, words `RESCUE` in `mobile/src/words.ts`, rules in `mobile/src/rescue.ts`; R5, the approved "V3RDE Rescue Mockup")

- On Give, a card that expired with nobody grabbing it has the red "Rescue" (T9: one word, a red pill). X49h (T9, Oct 31: "the same size as the timer pill"): a `SmallPill` at the right, under the time pill; Delete is the X on the photo. A tap opens the rescue popup.
- The popup is a `Popup small` (phone: the small sheet, as high as its content, at most `size.sheetLarge`; wide: the small dialog, at most `size.dialogMax` of the window height). Title (`type.title`): "Find a place for it" until places come; then "Take it to a food program" (drop-off) or "Ask a food program first" (bulk, 30 servings or more). The listing title under it in `muted`, `space.hair` under the title and `space.gutter` over the content. "Close" (secondary, `grow`) `space.gutter` under the content.
- Looking: "Looking for places near the pickup address…" (`muted`), while the app asks for the job once a second.
- Places: the line "Nobody picked this up in time. …" (drop-off) or "This is a large amount. …" (bulk) in `fg` (`Body`: the frosted layer lowers `muted`), then one card for each place, `space.item` apart, in a scroll view: `card` fill, `size.border` `line`, `radius.card`, `space.group` padding, `space.hair` between lines: the name (`type.button`, `fg`), the type and distance ("Food bank · 0.8 mi") and the address in `muted`, then a row of `TextLink touch` links `space.gutter` apart: "Directions", "Call", and for bulk "Email". The credit line from the answer in `muted` under the cards.
- No ZIP: "The pickup address has no ZIP code. …" (`muted`), the field "ZIP code" (example "For example 80219"), "Enter 5 digits, for example 80219" (`ErrorText`) when it is not one, and the primary pill "Search", `space.gap` apart. None: "No place was found near the pickup address." Failed: "Could not find places. Try again." (`ErrorText`) and the primary pill "Try again".
- The links open outside the app (a new tab on the web): only a Google Maps directions link, `tel:` and `mailto:` (rules `safeLink`).
- Note (T9, after the merge to `services`): "Delete" on a listing card becomes an X at the top right of the photo, in a round button with an opaque disc.
## Capture (`mobile/src/screens/Capture.tsx`, words in `mobile/src/words.ts`, rules in `mobile/src/capture.ts`)

- Give: "+ New listing" (a primary pill) at the bottom of a phone screen (`space.item` above and below) or next to the title on a wide screen. The flow opens by itself when the giver has nothing up.
- The camera (the approved X25 shots): phone: full screen on `cameraBg`, ✕ at `camera.edge`, the hint pill (`Hint`), upload at `camera.side` × `camera.uploadBottom`, the shutter. Wide screen: `CardDialog` with the live picture (`camera.ratio`, `radius.card`, the shutter `camera.cardShutterBottom` from its bottom), the pills "Upload a photo" and "Cancel" `space.gutter` under it, and the hint in `muted` `space.gap` under them. On the web an image can be dropped on the window; while it is over the window the picture has a `size.focusBorder` `accent` border and the hint says "Drop to use this image".
- After the shutter, each step is a small `Sheet` over the photo: "Use this photo?" (with "Barcode …" after a barcode read; "Try again?" with the error line after a retry answer), "Analyzing… n.n s" with the progress bar `space.bar` under it (it fills in the 75 s the app waits), and one sheet for each answer code (not food, no name for one or for several items, already listed, the daily limit). A sheet with a line has the pills `space.gutter` under it; without a line, `space.foot`.
- Esc (web) and Back go one step back; the photo stays until Retake.

## Review (`mobile/src/screens/Review.tsx`; the approved X32 mockup)

- A large `Sheet` over the photo (wide: the photo at the left, `space.foot` to the fields). One item: "Your listing", the field "Title", the source line (`space.inset` under it: "From the barcode …" or "The scanner says: …"), "Servings" with − and + (`size.iconBig`), the field "Pickup address and instructions" (R5; example "12 Main St, Denver, CO 80219. Front desk": the rescue reads the ZIP after the state code). Several items: "n items found", "Check each item. Remove what you do not want to share.", a `ReviewCard` for each item with the item photo of the server, and "Pickup address and instructions (for all items)".
- The pills "Start over" and "Post listing" / "Post n listings" (busy: "Posting…"); while several post, "Posting i of n…" shows over them. A failed item stays with "This item did not post. It stays here."; the title says "n items not posted".
- The form checks come before any request (`capture.ts`: title 1 to 80, servings 1 to 99, pickup 1 to 200). After the post, a toast: "Listed. It stays up for 30 minutes." or "n listings are posted. They stay up for 30 minutes."
- X49 (T9, Oct 30, option A): wide screens: the scan photo fills the height of the left column (`size.scanPhoto`, `radius.card`). A window of at least `size.dialogExtraWide` + 2 × `space.gutter`: a third column (`size.detailsColumn`, `card`, `radius.card`, `space.group` padding) shows the details of the item whose Details pill is on, with a Change pill (glyph pencil); with no item: "Tap Details on an item to see what the scan found." The dialog is then `size.dialogExtraWide`. Phone (and a narrower wide window): Details opens a small `Popup`: the item title, the details, and the pills "Change" and "Close".
- The details (rules in `mobile/src/details.ts`): one short sentence, then the description: "Ready to eat, vegetarian, in bulk, about 1.2 kg." A part that is missing is left out. The cost is never shown. The box opens in `motion.growMs`, then the text types in (`TypedText`).
- Change: the choices Condition (Ready to eat, Needs cooking, Sealed), Food (Vegetarian, Meat or fish) and Portions (Bulk, Single) as choice chips (a tap on the chosen chip clears it), the field "Weight (kg)" (0 to 1000), and the pills Cancel and Done. Change and Done are locked while the listings post. If the user clears all the details the scanner gave, the Details pill stays ("No details now. Tap Change to add them."). Done writes the item details (they post with the listing); each change goes in the label report (fields condition, diet, portions, weight).
- X49e: under the pickup field (once, also for several items), the pickup place, `space.group` above, `space.inset` in: "Pickup place" (`muted`), "Your location now" (body), "Receivers see how far away it is. They do not see this place on a map." (`muted`), and the text link "Use a ZIP code". With a ZIP: the field "Pickup ZIP code", "Location is off. People near this ZIP see the food." and the text link "Use my location". Post asks the browser for the location; if it is off, the form changes to the ZIP field with "Location is off. People near this ZIP see the food. Type the 5-digit pickup ZIP code."

## Touch-up (`mobile/src/screens/TouchUp.tsx`, rules in `mobile/src/touchup.ts`)

- In each review card with an item photo of the server (and under the source line of the one-item form): the `ChipBtn` "Touch up" (glyph sparkle; X48r: one touch-up, the auto one), with the `ChipBtn` "Details" (glyph info) next to it when the item has details. Details shows in every touch-up state; an item with no photo has Details only. A tap starts a touch-up job; "Touching up the photo…" with a spinner shows while it runs (the app asks every 2 s, at most 2 minutes).
- When the edit passed the food check: "Choose the photo to post:" and two photos side by side (`size.choiceThumb`, `radius.photo`), "Original" and "Touched up"; the chosen one has a `size.focusBorder` `accent` border. The original is chosen until the user taps the edit (a radio group).
- When the check saw a change to the food: "The photo stays as it is: the check saw a change to the food." A failed job, the daily limit or a busy service each have one line; the Touch up pill stays, so the user can try again.
- Post waits for a touch-up that runs ("Wait for the touch-up to end, then post.").

## Chat bar (`mobile/src/screens/Voice.tsx`, rules in `mobile/src/voice.ts`; X48s, the approved "V3RDE Chat Bar Mockup")

- Under the list, above the pills (`space.gap` above): the microphone (`IconBtn`, `size.iconBig`, glyph mic) and a field "Talk or type a change"; with text in it, a "Send" field button. While a clip runs: a `card` box with a `size.border` `accent` border, the stop glyph, "Listening… 0:04", "Tap to stop. It stops by itself after 1 minute." and a `Progress` bar to the limit. While the service reads: a spinner and "Reading the change…".
- On a wide screen the scan photo stays in place; only the list column scrolls, and the chat bar is under it.
- The changes go into the list at once. The chat is in the scroll body under the list: the user's line on `iconFill` ("You said" / "You typed"), the app's answer on a `card` with a `line` border ("3 changes made", one line for each change), `radius.card`, `space.item` padding. Undo (a text link) is on the last change only, while the list is as that change left it. A changed card and the pickup show "Changed in the chat: …" in `link`.
- Before the first line: "Try: “6 servings”, “remove item 2”." The chat is cleared after a post. Post waits while a line is being read.

## Mockups

A new or changed screen is shown to T9 before its code change: rendered from the real parts in a copy of the app, exported for the web and captured with Playwright (phone 390 × 844 at 3×, desktop 1440 × 900 at 2×), with real photos. The review page shows phone shots at true size. Nothing is drawn by hand, and nothing is stretched. An independent check compares the shots with the approved shots.
