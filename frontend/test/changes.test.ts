import { describe, expect, it } from "vitest";
import { patchLines, patchSections } from "../src/lib/changes.js";

const patch = `diff --git a/old.txt b/old.txt
index 1..2 100644
--- a/old.txt
+++ b/old.txt
@@ -1,2 +1,3 @@
 one
-two
+TWO
+three
diff --git a/gone.txt b/gone.txt
deleted file mode 100644
--- a/gone.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
diff --git a/a b.txt b/c d.txt
similarity index 100%
rename from a b.txt
rename to c d.txt
diff --git a/notes.md b/notes.md
new file (untracked)
--- /dev/null
+++ b/notes.md
@@ -0,0 +1,1 @@
+--- not a header
`;

describe("patch sections", () => {
  it("cuts a diff per file, named by the file after the change", () => {
    const sections = patchSections(patch);
    expect([...sections.keys()]).toEqual(["old.txt", "gone.txt", "c d.txt", "notes.md"]);
    expect(sections.get("gone.txt")).toContain("-bye");
  });

  it("reads added and removed lines only inside a hunk", () => {
    const kinds = patchLines(patchSections(patch).get("old.txt")!).map((line) => line.kind);
    expect(kinds).toEqual(["meta", "meta", "meta", "meta", "hunk", "context", "del", "add", "add"]);
    const untracked = patchLines(patchSections(patch).get("notes.md")!);
    expect(untracked.at(-1)).toEqual({ text: "+--- not a header", kind: "add" });
  });
});
