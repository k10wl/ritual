export type EditOp = "match" | "replace" | "insert" | "delete";

export interface Edit {
    op: EditOp;
    oldIdx: number; // -1 for insert
    newIdx: number; // -1 for delete
    ch: string;     // match/delete: from prev; replace/insert: from next
}

export interface EditGroup {
    op: EditOp;
    edits: Edit[];
}

export const diffMatrix = (a: string, b: string): Edit[] => {
    const m = a.length;
    const n = b.length;
    const M: number[][] = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
    for (let i = 0; i <= m; i++) M[i][0] = i;
    for (let j = 0; j <= n; j++) M[0][j] = j;
    for (let i = 1; i <= m; i++) {
        for (let j = 1; j <= n; j++) {
            if (a[i - 1] === b[j - 1]) {
                M[i][j] = M[i - 1][j - 1];
            } else {
                M[i][j] = 1 + Math.min(M[i - 1][j - 1], M[i - 1][j], M[i][j - 1]);
            }
        }
    }
    const edits: Edit[] = [];
    let i = m;
    let j = n;
    while (i > 0 || j > 0) {
        if (i > 0 && j > 0 && a[i - 1] === b[j - 1]) {
            edits.unshift({ op: "match", oldIdx: i - 1, newIdx: j - 1, ch: a[i - 1] });
            i--;
            j--;
        } else if (i > 0 && j > 0 && M[i][j] === M[i - 1][j - 1] + 1) {
            edits.unshift({ op: "replace", oldIdx: i - 1, newIdx: j - 1, ch: b[j - 1] });
            i--;
            j--;
        } else if (i > 0 && M[i][j] === M[i - 1][j] + 1) {
            edits.unshift({ op: "delete", oldIdx: i - 1, newIdx: -1, ch: a[i - 1] });
            i--;
        } else {
            edits.unshift({ op: "insert", oldIdx: -1, newIdx: j - 1, ch: b[j - 1] });
            j--;
        }
    }
    return edits;
};

export const groupEdits = (edits: Edit[]): EditGroup[] => {
    const groups: EditGroup[] = [];
    for (const e of edits) {
        const last = groups[groups.length - 1];
        if (last && last.op === e.op) {
            last.edits.push(e);
        } else {
            groups.push({ op: e.op, edits: [e] });
        }
    }
    return groups;
};
