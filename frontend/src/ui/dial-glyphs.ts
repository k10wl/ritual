import { Play, Square, X as XIcon, Download, Upload, BrainCog, Unplug, FolderInput } from "lucide";
import svgpath from "svgpath";
import { shapeToD, type LucideIcon } from "./lucide-shape";

export type DialGlyph =
    | "play" | "stop" | "x" | "download" | "upload" | "brain-cog" | "unplug"
    | "folder-input" | null;

export const compoundD = (icon: LucideIcon): string =>
    icon.map(shapeToD).filter(Boolean).map((d) => svgpath(d).abs().toString()).join(" ");

export const GLYPHS: Record<Exclude<DialGlyph, null>, string> = {
    play:           compoundD(Play as LucideIcon),
    stop:           compoundD(Square as LucideIcon),
    x:              compoundD(XIcon as LucideIcon),
    download:       compoundD(Download as LucideIcon),
    upload:         compoundD(Upload as LucideIcon),
    "brain-cog":    compoundD(BrainCog as LucideIcon),
    unplug:         compoundD(Unplug as LucideIcon),
    // design-log/055 addendum — relocate (PhaseRelocating): moving CONTENT
    // into a user-chosen destination folder, distinct from download/upload's
    // network-direction arrows since this is a local-to-local move.
    "folder-input": compoundD(FolderInput as LucideIcon),
};

export const dFor = (g: DialGlyph): string => (g ? GLYPHS[g] : "");
