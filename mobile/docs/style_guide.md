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
| `size.dialogWide` | 860 | X32: 860 | a dialog with the photo on the left |
| `size.dialogSmall` | 440 | X32: 440 | a dialog with a title, a line and two pills |
| `size.dialogCamera` | 640 | X25: 640 | the camera dialog on a wide screen (a 4:3 photo, 608 pt wide) |
| `size.dialogMax` | 0.869 | X32: 782 of 900 pt | a dialog is at most this share of the window height |
| `size.blur` | 40 | X32, X25 (frost check) | expo-blur intensity of the frosted layer |
| `size.progressH` | 6 | X25: 6.0 | the progress bar height |
| `size.focusBorder` | 2 | Ref-login: 2.00 | the border of a field while it has the focus (in `accent`) |
| `size.fieldSlot` | 76 | Ref-login | field text to the right border when a text button (Show) sits in the field |
| `size.loginTop` | 0.14 | Ref-login | the sign-in title from the top of a phone screen, as a share of the screen width (rounded to a whole pt) |

## Glyphs (`glyph`)

The glyphs are SVG paths with round ends (react-native-svg, MIT, https://github.com/software-mansion/react-native-svg), drawn in a square box in pt. Each path ends half a stroke inside its box, so the round ends touch the box edges.

| Token | Value | Source | Use |
|---|---|---|---|
| `glyph.box` | 12 | X32: ✕ and + 11.5–11.7 (ink), − 12.0 wide | the box of close, minus and plus |
| `glyph.uploadBox` | 15 | X25: 15.0 × 15.1 (ink) | the box of upload |
| `glyph.stroke` | 1.67 | X32: − 1.68 pt (phone), 1.65 pt (desktop) | the line width |
| `glyph.close` | M0.835 0.835 L11.165 11.165 M11.165 0.835 L0.835 11.165 | X32 | ✕ (remove an item; close the camera) |
| `glyph.minus` | M0.835 6 H11.165 | X32 | − (fewer servings) |
| `glyph.plus` | M0.835 6 H11.165 M6 0.835 V11.165 | X32 | + (more servings) |
| `glyph.upload` | M7.5 0.835 V10.8 M4.25 4 L7.5 0.835 L10.75 4 M0.835 11 V12.2 A2.2 2.2 0 0 0 3.035 14.165 H11.965 A2.2 2.2 0 0 0 14.165 12.2 V11 | X25 | upload: an arrow up over an open tray with round bottom corners |

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
| `motion.rowStepMs` | 40 | Choice | each row of a sheet starts 40 ms after the row before it |
| `motion.rowRise` | 8 | Choice | a row fades in and rises this far (pt) |
| `motion.rowMs` | 220 | Choice | how long a row takes (ms) |
| `motion.sheetSpeed` | 14 | Choice | the spring of a sheet (React Native `Animated.spring` speed) |
| `motion.sheetBounce` | 0 | Choice | its bounciness (no overshoot) |
| `motion.sheetShift` | 0.2 | Choice | a sheet starts this share of the window height below its place |
| `motion.handoffMs` | 300 | Ref-login | wide screens: the sign-in card fades out in this time after sign-in |
| `motion.handoffLift` | 40 | Ref-login | and rises this far (pt) |

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

| Function | Width (pt) | Kind | Popups | Source |
|---|---|---|---|---|
| `layoutFor` | below 600 | compact | bottom sheets | Platform (Material 3 window size classes) |
| `layoutFor` | 600 to 839 | medium | centred dialogs | Platform |
| `layoutFor` | 840 and more | expanded | centred dialogs | Platform |

## Parts (`mobile/src/ui.tsx`)

- **Text:** `Title` (`type.title`, `fg`), `Body` (`type.body`, `fg`), `Muted` (`type.body`, `muted`), `ErrorText` (`type.body`, `danger`, `space.gap` above it, read out as an alert).
- **GlyphIcon:** a glyph from `glyph` in its box, `glyph.stroke` wide, round ends.
- **IconBtn:** a round `iconFill` disc with a glyph in `fg`: `size.iconBig`, or `size.iconSmall` inside a review card (its touch area grows to `size.iconBig`). `opacity.pressed` while pressed.
- **Pill:** `size.control` high, fully round, `size.border`. `primary` = `accent` fill with `onAccent` text; `secondary` = `card` fill, `line` border, `fg` text. With `grow`, two pills share a row, `space.item` apart: secondary on the left, primary on the right. While busy: a spinner in place of the text, and `opacity.busy`; a tap does nothing, and the pill keeps the keyboard focus. With the mouse on it (web): `opacity.hover`.
- **FieldButton:** a text button inside a field (Show, Hide): `type.button` in `link`, `space.item` at the sides and `space.group` above and below (a 44 pt touch area, Platform), `space.gap` from the right border; it has its own name for a screen reader ("Show password").
- **TextLink:** an action in a line of text ("Create one"): `type.button` in `link`, underlined; a screen reader reads it as a button.
- **Doodles:** the doodle wallpaper; it fills its parent under the content and draws for the size of its parent; the rule-30 pattern moves up one row every `doodle.stepMs` (still with 'reduce motion'); no touches; hidden from screen readers.
- **Row:** controls in a line, `space.item` apart; `tight` = `space.gap` apart (inside a card).
- **Field:** `size.control` high, fully round, `card` fill, `size.border` `line` border, text `size.fieldPadX` from the sides, `placeholder` for the example text. With the focus: a `size.focusBorder` border in `accent` (no browser focus ring). With `right` (a FieldButton): text `size.fieldSlot` from the right border. The label is also the name of the input for a screen reader. With several lines: `size.controlTall`, `radius.card`, text `size.fieldPadTop` from the top. A label is `Muted`, `space.inset` in, `space.item` above it, `space.hair` to the field.
- **ReviewCard:** one found item: `card` fill, `size.border` `line`, `radius.card`, `space.inset` padding, `space.item` above it. The `size.thumb` photo (`radius.photo`), `space.item` to two `tight` rows `space.gap` apart: the title field with ✕ (remove), and − servings +, with `size.iconSmall` buttons. An error line goes under the rows.
- **Rise:** each row of a sheet fades in and rises `motion.rowRise`, `motion.rowStepMs` after the row before it, in `motion.rowMs`.
- **Sheet:** the photo behind it with the shade over it (`shadeTop`, `shadeMid` at `shade.mid`, `shadeEnd`), then a frosted layer (the blur of the photo, `size.blur`, `blurTint`, with `sheetTint` over it). Compact width: a bottom sheet, `radius.sheet` top corners, the grab bar (`size.handleW` × `size.handleH`, `handle`, `size.handleTop` from the top, `space.gutter` to the content), `space.gutter` sides, `space.foot` under the last pill. `large` = as tall as its content, at most `size.sheetLarge` of the screen height; `small` = at least `size.sheetSmall`. Medium and expanded width: a centred dialog, `radius.sheet` corners, `space.pad` padding, at most `size.dialogMax` of the window height; `wide` = `size.dialogWide` with the photo on the left (`space.foot` to the list), else `size.dialogSmall`. It rises with a spring (`motion.sheetSpeed`, `motion.sheetBounce`, from `motion.sheetShift` of the window height).
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

## Mockups

A new or changed screen is shown to T9 before its code change: rendered from the real parts in a copy of the app, exported for the web and captured with Playwright (phone 390 × 844 at 3×, desktop 1440 × 900 at 2×), with real photos. The review page shows phone shots at true size. Nothing is drawn by hand, and nothing is stretched. An independent check compares the shots with the approved shots.
