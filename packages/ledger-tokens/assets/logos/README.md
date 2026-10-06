# Logos

The mark is two identical rounded hook strokes, one turned 180° so they interlock. It carries over from V1. The wordmark is "Memax" set in **Gloock**, a high-contrast display serif chosen because its weight matches the mark's heavy strokes. The capital M is the same in the logo and in prose. These are single-ink SVGs, because `<img>` can't inherit colour, so pick the file by its ground. The lockups have the text outlined, so they need no font.

- `memax-lockup-ink.svg`: the mark and "Memax" in ink `#151b18` (the `ink` token in Paper). Use it on `paper`, `sheet`, `card` and white.
- `memax-lockup-paper.svg`: the same in paper `#f4f7f5`. Use it on `ink`, `night` and Carbon surfaces.
- `memax-mark-ink.svg` and `memax-mark-paper.svg`: the mark alone, for favicons, the app rail and anywhere under 24px.
- `memax-wordmark-v1-ink.svg`: the retired V1 drawn wordmark, kept for reference only.

Lockup geometry: the mark's ink is 1.04× the cap height of the M and centred on it, so it overshoots the cap line and the baseline equally. The gap from the mark to the M's serif is 0.30× the cap height, and the name is tracked −0.015em. The lockup's box is tight to the ink (about 5.96:1).

- Keep clear space equal to half the lockup's height on every side.
- Don't rebuild it from the mark and live text.
- Don't recolour it in `seal` or any state colour, or draw it as a stroked outline.
- Don't rotate it, lowercase it or set it in another face.
