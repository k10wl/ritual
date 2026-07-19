// Pure, dependency-free half of the lucide icon-node → SVG path conversion.
// Split out of dial-glyphs.ts so it can be unit-tested without pulling in
// svgpath: svgpath is a bare-CommonJS package (`module.exports = SvgPath`,
// no `export` keyword) that Vite's bundler happily interops for the real
// app, but @web/test-runner's plain nodeResolve serves CJS files unconverted
// and browser-native ESM then refuses the import ("does not provide an
// export named 'default'"). Keeping the highest-regression-risk logic
// (shape-tag → path conversion) in an svgpath-free module means it stays
// testable regardless of that test-runner gap. design-log/050 §B.
export type LucideChild = readonly [string, Record<string, string>];
export type LucideIcon = ReadonlyArray<LucideChild>;

// lucide ships icons as [tag, attrs] arrays (framework-agnostic "icon node"
// format) rather than pre-rendered SVG strings, so each shape gets converted
// to an equivalent path `d` here. Only the tags lucide's icon set actually
// uses today are handled — a future lucide release that redraws an icon
// with a tag not covered here (e.g. polygon/polyline/ellipse) silently
// produces "" for that shape instead of throwing. See lucide-shape.test.ts,
// which exercises this against the real installed lucide data so a drift
// like that fails a test instead of shipping an invisible dial glyph.
export const shapeToD = ([tag, a]: LucideChild): string => {
    if (tag === "path") return a.d;
    if (tag === "line") return `M${a.x1} ${a.y1}L${a.x2} ${a.y2}`;
    if (tag === "circle") {
        const cx = +a.cx, cy = +a.cy, r = +a.r;
        return `M${cx - r} ${cy}A${r} ${r} 0 1 0 ${cx + r} ${cy}A${r} ${r} 0 1 0 ${cx - r} ${cy}Z`;
    }
    if (tag === "rect") {
        const x = +a.x, y = +a.y, w = +a.width, h = +a.height;
        const r = +(a.rx ?? a.ry ?? 0);
        if (!r) return `M${x} ${y}h${w}v${h}h${-w}Z`;
        return `M${x + r} ${y}H${x + w - r}A${r} ${r} 0 0 1 ${x + w} ${y + r}` +
               `V${y + h - r}A${r} ${r} 0 0 1 ${x + w - r} ${y + h}` +
               `H${x + r}A${r} ${r} 0 0 1 ${x} ${y + h - r}` +
               `V${y + r}A${r} ${r} 0 0 1 ${x + r} ${y}Z`;
    }
    return "";
};
