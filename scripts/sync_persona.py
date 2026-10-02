#!/usr/bin/env python3
"""Seal celeste-persona-container's CLI profiles into celeste-cli; install lore locally.

    python3 scripts/sync_persona.py             # rebuild at the pins, seal, install lore
    python3 scripts/sync_persona.py --check     # rebuild at the pins and compare (needs the key)
    python3 scripts/sync_persona.py --no-lore   # leave the local lore alone
    PERSONA_CORE_COMMIT=<sha> python3 scripts/sync_persona.py   # move a pin

Needs checkouts of celeste-core-persona and celeste-persona-container
(PERSONA_CORE / PERSONA_CONTAINER; default: siblings of this repo) and the
persona key (CELESTE_PERSONA_KEY, or the file CELESTE_PERSONA_KEY_FILE,
default ~/.celeste/persona.key). Both repos are built from temporary clean
worktrees at the pinned commits (cmd/celeste/prompts/persona/SOURCE.json),
in a temporary directory outside this repository. The plaintext never
enters the repository: scripts/personaseal reads it there and writes only
ciphertext and SOURCE.json. Lore goes to ~/.celeste/persona-lore
(PERSONA_LORE_DIR), never into the repository (W5 rulings 3, 5).
"""
import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
DEST = REPO / "cmd" / "celeste" / "prompts" / "persona"
HANDOFF = REPO / "docs" / "slider-agent-handoff.md"
PROFILES = ("full", "spine", "lite", "off")
NOT_LORE = ("platform_rules/",)  # stream formatting rules, not lore


def git(repo, *args):
    return subprocess.run(["git", "-C", str(repo), *args], check=True,
                          stdout=subprocess.PIPE, text=True).stdout.strip()


def shown(path):
    """A path for printing: ~/… under the home directory, else its name."""
    try:
        return "~/" + Path(path).resolve().relative_to(Path.home().resolve()).as_posix()
    except ValueError:
        return Path(path).name


def inside_repo(path):
    p = Path(path).resolve()
    return p == REPO.resolve() or REPO.resolve() in p.parents


def run_builder(cmd):
    """Run the container's builder, echoing its report without the lines that
    name temporary paths (source, template, wrote)."""
    res = subprocess.run(cmd, check=True, stdout=subprocess.PIPE, text=True)
    for line in res.stdout.splitlines():
        if not line.startswith(("source:", "template:", "wrote ")):
            print(line)


def build(core, container, core_commit, container_commit, with_lore, tmp):
    """Build at the pins inside tmp. Returns (plain dir, lore dir or None, handoff bytes)."""
    core_wt, cont_wt, out = tmp / "core", tmp / "container", tmp / "dist"
    git(core, "worktree", "add", "--detach", str(core_wt), core_commit)
    git(container, "worktree", "add", "--detach", str(cont_wt), container_commit)
    try:
        builder = [sys.executable, str(cont_wt / "build_container.py"),
                   "--source", str(core_wt), "--out", str(out)]
        run_builder(builder + ["--target", "cli"])
        plain = tmp / "plain"
        plain.mkdir(mode=0o700)
        for p in PROFILES:
            data = (out / "cli" / f"celeste_{p}.json").read_bytes()
            if json.loads(data)["source_commit"] != core_commit:
                raise SystemExit(f"{p}: built from another commit than the pin")
            (plain / f"celeste_{p}.json").write_bytes(data)
        lore = None
        if with_lore:
            subprocess.run(builder, check=True, stdout=subprocess.DEVNULL)
            lore = tmp / "lore"
            lore.mkdir(mode=0o700)
            rag = out / "rag"
            for f in sorted(rag.rglob("*"), key=lambda x: x.as_posix()):
                rel = f.relative_to(rag).as_posix()
                if f.is_dir() or rel.startswith(NOT_LORE):
                    continue
                (lore / rel).parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(f, lore / rel)
        handoff = (core_wt / "docs" / "slider-agent-handoff.md").read_bytes()
        return plain, lore, handoff
    finally:
        git(core, "worktree", "remove", "--force", str(core_wt))
        git(container, "worktree", "remove", "--force", str(cont_wt))


def personaseal(command, plain, *extra):
    subprocess.run(["go", "run", "./scripts/personaseal", command, "-repo", str(REPO),
                    "-in", str(plain), *extra], check=True, cwd=REPO)


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--check", action="store_true", help="compare instead of writing")
    ap.add_argument("--no-lore", action="store_true", help="do not install lore")
    a = ap.parse_args()
    core = Path(os.environ.get("PERSONA_CORE", REPO.parent / "celeste-core-persona"))
    container = Path(os.environ.get("PERSONA_CONTAINER", REPO.parent / "celeste-persona-container"))
    pins = json.loads((DEST / "SOURCE.json").read_text()) if (DEST / "SOURCE.json").is_file() else {}
    if a.check:
        core_ref, cont_ref = pins.get("core_commit"), pins.get("container_commit")
    else:
        core_ref = os.environ.get("PERSONA_CORE_COMMIT") or pins.get("core_commit")
        cont_ref = os.environ.get("PERSONA_CONTAINER_COMMIT") or pins.get("container_commit")
    if not core_ref or not cont_ref:
        raise SystemExit("no pins: set PERSONA_CORE_COMMIT and PERSONA_CONTAINER_COMMIT for the first sync")
    core_commit = git(core, "rev-parse", "--verify", core_ref + "^{commit}")
    cont_commit = git(container, "rev-parse", "--verify", cont_ref + "^{commit}")
    lore_dir = Path(os.environ.get("PERSONA_LORE_DIR", Path.home() / ".celeste" / "persona-lore"))
    if inside_repo(lore_dir):
        raise SystemExit("PERSONA_LORE_DIR is inside the repository; lore never goes there")
    with tempfile.TemporaryDirectory(prefix="persona-sync-") as tmp:
        tmp = Path(tmp)
        if inside_repo(tmp):
            raise SystemExit("the temporary directory is inside the repository; set TMPDIR elsewhere")
        with_lore = not (a.check or a.no_lore)
        plain, lore, handoff = build(core, container, core_commit, cont_commit, with_lore, tmp)
        if a.check:
            personaseal("check", plain)
            if not HANDOFF.is_file() or HANDOFF.read_bytes() != handoff:
                raise SystemExit("FAIL: docs/slider-agent-handoff.md differs from the pin (run make sync-persona)")
            return
        personaseal("seal", plain, "-core", core_commit, "-container", cont_commit)
        HANDOFF.write_bytes(handoff)
        if lore is not None:
            if lore_dir.exists():
                shutil.rmtree(lore_dir)
            shutil.copytree(lore, lore_dir)
            os.chmod(lore_dir, 0o700)
            n = sum(1 for _ in lore_dir.rglob("*.md"))
            print(f"lore: {n} files installed in {shown(lore_dir)} (local only, never committed)")


if __name__ == "__main__":
    main()
