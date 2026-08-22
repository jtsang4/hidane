import { describe, expect, it } from "vitest";
import { mkdtemp, writeFile, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execa } from "execa";
import { createWorkItem } from "../src/kernel/workItems.js";
import { listEvents } from "../src/kernel/events.js";

async function makeGitRepo(): Promise<string> {
  const dir = await mkdtemp(join(tmpdir(), "hidane-repo-"));
  await execa("git", ["-C", dir, "init", "-b", "main"], { stdin: "ignore" });
  await writeFile(join(dir, "README.md"), "# test repo\n");
  await execa("git", ["-C", dir, "add", "-A"], { stdin: "ignore" });
  await execa(
    "git",
    ["-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init"],
    { stdin: "ignore" },
  );
  return dir;
}

describe("worktree workspace provider", () => {
  it("creates a git worktree workspace on a dedicated branch", async () => {
    const repo = await makeGitRepo();
    const item = await createWorkItem("Repo goal", "kernel", { repo });

    // A worktree's .git is a file pointing at the main repo.
    const gitPointer = await stat(join(item.workspace, ".git"));
    expect(gitPointer.isFile()).toBe(true);

    const branches = await execa("git", ["-C", repo, "branch", "--list", `hidane/${item.id}`], {
      stdin: "ignore",
    });
    expect(branches.stdout).toContain(`hidane/${item.id}`);

    const created = await listEvents({ kind: "work_item.created", workItemId: item.id });
    expect(created[0]!.payload["provider"]).toBe("worktree");
    expect(created[0]!.payload["branch"]).toBe(`hidane/${item.id}`);
  });

  it("falls back to a plain dir for non-repo paths", async () => {
    const item = await createWorkItem("Plain goal", "kernel", { repo: "/nonexistent/repo" });
    const created = await listEvents({ kind: "work_item.created", workItemId: item.id });
    expect(created[0]!.payload["provider"]).toBe("dir");
  });
});
