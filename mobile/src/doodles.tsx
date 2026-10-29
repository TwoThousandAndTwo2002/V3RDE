// The food doodles (a wallpaper), written new for V3RDE (NEW_WORK_UI.md, UI 7; mobile/docs/style_guide.md
// "Doodles"). The drawings are new line art; the cellular automaton rule 30 (automata.ts) makes each cell live
// or dead, and a dead cell's drawing is fainter. Decorative: no touches, hidden from screen readers. It fills its
// parent and draws for the size of its parent.
import React, { useEffect, useMemo, useState } from "react";
import { AccessibilityInfo, StyleSheet, View, useColorScheme } from "react-native";
import Svg, { G, Path } from "react-native-svg";
import { doodle, useColors } from "./theme";
import { startGrid, stepUp, wallpaper, type Grid } from "./automata";

/** The drawings: SVG paths in a box of doodle.units, drawn with round ends. */
export const DRAWINGS: readonly string[] = [
  "M12 7.5c-2-1.6-6.2-1.2-6.6 3.4-.4 4.8 2.6 9.2 4.4 9.6 1 .2 1.4-.6 2.2-.6s1.2.8 2.2.6c1.8-.4 4.8-4.8 4.4-9.6C18.2 6.3 14 5.9 12 7.5zM12 7.5c0-1.8.6-3 1.6-3.8M13.6 5.8c1.2-1.4 3-1.8 4.2-1-1 1.4-2.6 1.8-4.2 1z", // apple
  "M12 5c-1.6 0-2.2 1.4-2.2 3.2 0 1.8-3.8 3.6-3.8 7.6a6 6 0 0 0 12 0c0-4-3.8-5.8-3.8-7.6C14.2 6.4 13.6 5 12 5zM12 5V2.6M12.4 3.6c1.2-1 2.6-1 3.4-.2", // pear
  "M5 19.5C7 15 11 10.5 14.5 8.4a2.6 2.6 0 0 1 3 3C15.4 14.9 10.9 18.9 6.5 21c-1.3.6-2.1-.2-1.5-1.5zM15.8 8l.4-4.6M16.8 9.2l4.4-1.4M16.4 8.4l3.4-3.4M9.6 15.4l1.6 1.4M12.2 12.6l1.4 1.2", // carrot
  "M5 11.4C2.8 11.4 2.8 7 6 7h12c3.2 0 3.2 4.4 1 4.4V19H5zM9.4 8.8l-1.2 2.2M13 8.8l-1.2 2.2M16.6 8.8l-1.2 2.2", // bread
  "M3 10.5a9 2.6 0 1 0 18 0 9 2.6 0 1 0-18 0M3 10.5c.6 5.6 4.4 9 9 9s8.4-3.4 9-9M13.5 3l3 6.2M17.5 2.6l1 6.6", // bowl
  "M6 7.5h12l-1.6 13.5H7.6zM5 4.5h14v3H5zM9 2.5h6v2M7.2 12.5h9.6", // cup
  "M4 5.2c5.2-2 11-1.4 16.4 2.6L12.4 21zM5.6 7.6c4.4-1.4 9-1 13 1.4M10 11.4a1.2 1.2 0 1 0 2.4 0 1.2 1.2 0 1 0-2.4 0M12.2 15.6a1 1 0 1 0 2 0 1 1 0 1 0-2 0", // pizza
  "M2.8 12c4.2-6 11-6 14.8 0-3.8 6-10.6 6-14.8 0zM17.6 12l3.6-3.4v6.8zM7 11.2v.1M11 9.4c1 1.6 1 3.6 0 5.2", // fish
  "M5.6 9.2c.8-4 6-5.4 9.2-3.4s6.4 2.6 5.4 6.6-2.4 7.2-7.4 7.2-9.2-1-8.4-5c.4-2 .6-3.6 1.2-5.4zM10 12.4a2.6 2.6 0 1 0 5.2 0 2.6 2.6 0 1 0-5.2 0", // egg
  "M3 16.5l13.5-9 4.5 4.5v8.5H3zM3 16.5h18M8.5 18.4a.1.1 0 1 0 .2 0M14.5 14.2a.1.1 0 1 0 .2 0", // cheese
  "M4.6 17.2a3 3 0 1 0 6 0 3 3 0 1 0-6 0M13 18.2a3 3 0 1 0 6 0 3 3 0 1 0-6 0M7.6 14.2C8.8 9 11.6 5.6 15 3.6M16 15.2c0-4.2.2-8.2-1-11.6M15 3.6c1.6-.6 3.4 0 4.4 1.4-1.8.8-3.4.4-4.4-1.4z", // cherry
  "M3.5 12c1.5-4.5 5-7 8.5-7s7 2.5 8.5 7c-1.5 4.5-5 7-8.5 7s-7-2.5-8.5-7zM2.4 12h1.1M20.5 12h1.1M8.6 10c.9-1 2-1.6 3.2-1.8", // lemon
  "M5.5 8.5a2.5 2.5 0 1 0 5 0 2.5 2.5 0 1 0-5 0M10.5 8.5a2.5 2.5 0 1 0 5 0 2.5 2.5 0 1 0-5 0M15.5 8.5a2.5 2.5 0 1 0 5 0 2.5 2.5 0 1 0-5 0M8 13a2.5 2.5 0 1 0 5 0 2.5 2.5 0 1 0-5 0M13 13a2.5 2.5 0 1 0 5 0 2.5 2.5 0 1 0-5 0M10.5 17.5a2.5 2.5 0 1 0 5 0 2.5 2.5 0 1 0-5 0M13 6V3.2c1.2-.8 2.6-.8 3.6 0", // grapes
  "M6 12.4h12l-1.6 8H7.6zM5.4 12.4C4 12.4 4 8.2 7.2 8.2c.4-3 3-4.2 4.8-4.2s4.4 1.2 4.8 4.2c3.2 0 3.2 4.2 1.8 4.2M10 12.4l.4 8M14 12.4l-.4 8", // muffin
  "M14.6 4.6a5.2 5.2 0 0 1 4.8 7.4c-1.2 2.6-4.4 3.4-6.6 4.4L9.4 18a2 2 0 1 1-2.8 2.8 2 2 0 1 1-2.8-2.8 2 2 0 1 1 2.8-2.8L8.2 11.6c1-2.2 1.8-5.4 4.4-6.6.6-.3 1.3-.4 2-.4z", // drumstick
  "M12 21c-4-2-7.5-6.5-7-10.5.4-2.6 2.6-3.6 7-3.6s6.6 1 7 3.6c.5 4-3 8.5-7 10.5zM8.4 7l3.6-3.2 3.6 3.2M12 3.8V2.2M9.6 12v.2M14.4 12v.2M12 15.6v.2", // strawberry
  "M6.8 10.5l5.2 11 5.2-11zM5.8 10.5a6.2 6.2 0 0 1 12.4 0zM9 13.6l5.6 2.9M10.7 18l3.5-1.8", // icecream
  "M3 8.5h18a9 9 0 0 1-18 0zM5.6 8.5a6.4 6.4 0 0 0 12.8 0M9.4 11.4v.4M12 13.2v.4M14.6 11.4v.4", // watermelon
  "M4 10.5a8 5.5 0 0 1 16 0zM3.5 13.5h17M5 16.5h14v1a2.5 2.5 0 0 1-2.5 2.5h-9A2.5 2.5 0 0 1 5 17.5zM8.5 7.6v.1M12 6.6v.1M15.5 7.6v.1", // burger
  "M10 13.6l.5 7.4h3l.5-7.4M8.2 13.6a3.6 3.6 0 0 1-.4-7.1 4.2 4.2 0 0 1 8.4 0 3.6 3.6 0 0 1-.4 7.1z", // broccoli
];

const PLAN = {
  cell: doodle.cell, drawings: DRAWINGS.length, jitter: doodle.jitter, turn: doodle.turn, steps: doodle.turnSteps,
  seed: doodle.seed, density: doodle.rowDensity,
};

/**
 * The doodle wallpaper: it fills its parent, under the content. Every `doodle.stepMs` the rule-30 pattern moves up
 * one row (the drawings stay; only live and dead change). With "reduce motion" on, the pattern stands still.
 */
export const Doodles = () => {
  const col = useColors();
  const night = useColorScheme() === "dark";
  const [box, setBox] = useState({ w: 0, h: 0 });
  const [grid, setGrid] = useState<Grid>([]);
  const [still, setStill] = useState(true);
  useEffect(() => setGrid(startGrid(box.w, box.h, PLAN)), [box.w, box.h]);
  useEffect(() => {
    let live = true;
    AccessibilityInfo.isReduceMotionEnabled().then((on) => { if (live) setStill(on); }).catch(() => { if (live) setStill(false); });
    const sub = AccessibilityInfo.addEventListener("reduceMotionChanged", (on) => setStill(on));
    return () => { live = false; sub.remove(); };
  }, []);
  useEffect(() => {
    if (still) return;
    const timer = setInterval(() => setGrid((g) => stepUp(30, g)), doodle.stepMs);
    return () => clearInterval(timer);
  }, [still]);
  const marks = useMemo(() => (grid.length ? wallpaper(box.w, box.h, PLAN, grid) : []), [box.w, box.h, grid]);
  const scale = doodle.glyph / doodle.units, half = doodle.glyph / 2;
  return (
    <View pointerEvents="none" aria-hidden accessibilityElementsHidden importantForAccessibility="no-hide-descendants"
      onLayout={(e) => setBox({ w: e.nativeEvent.layout.width, h: e.nativeEvent.layout.height })}
      style={[StyleSheet.absoluteFill, { opacity: night ? doodle.opacityDark : doodle.opacity }]}>
      {box.w > 0 ? (
        <Svg width={box.w} height={box.h}>
          <G stroke={col.doodle} strokeWidth={doodle.stroke / scale} fill="none" strokeLinecap="round" strokeLinejoin="round">
            {marks.map((m, i) => (
              <Path key={i} d={DRAWINGS[m.drawing]} strokeOpacity={m.live ? 1 : doodle.faint}
                transform={`translate(${m.x - half} ${m.y - half}) rotate(${m.turn} ${half} ${half}) scale(${scale})`} />
            ))}
          </G>
        </Svg>
      ) : null}
    </View>
  );
};
