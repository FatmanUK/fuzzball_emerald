#!/usr/bin/env python3
"""Generate internal/muf/mlev_gen.go: each primitive's mucker-level floor.

Fuzzball guards privileged primitives with "if (mlev < N) abort_interp(...)"
inside each implementation. Those checks are a security surface, not just a
compatibility one, so they are extracted rather than transcribed.

    python3 internal/muf/internal/gen/gen_mlev.py [git-ref]
"""
import json
import re
import subprocess
import sys
import pathlib

REF = sys.argv[1] if len(sys.argv) > 1 else "origin/mother"

ROOT = pathlib.Path(subprocess.run(
    ["git", "rev-parse", "--show-toplevel"],
    capture_output=True, text=True, check=True).stdout.strip())
OUT = ROOT / "internal/muf/mlev_gen.go"

MODULES = ["p_array", "p_connects", "p_db", "p_error", "p_float", "p_math",
           "p_mcp", "p_misc", "p_props", "p_regex", "p_stack", "p_strings"]

# Symbolic levels used in the checks.
LEVELS = {"MLEV_APPRENTICE": 1, "MLEV_JOURNEYMAN": 2, "MLEV_MASTER": 3,
          "MLEV_WIZARD": 4, "MLEV_GOD": 4,
          "1": 1, "2": 2, "3": 3, "4": 4}


def show(path):
    return subprocess.run(["git", "show", f"{REF}:{path}"],
                          capture_output=True, text=True, check=True,
                          cwd=ROOT).stdout


def conditions(body):
    """Yield each if-condition in a function body, parentheses balanced.

    Matching to the first ")" is not enough: "if ((mlev < 4) && !permissions(x))"
    would be cut after "(mlev < 4", hiding the permission test that makes it a
    choice rather than a floor.
    """
    for m in re.finditer(r'\bif\s*\(', body):
        i = m.end() - 1
        depth = 0
        for j in range(i, len(body)):
            if body[j] == '(':
                depth += 1
            elif body[j] == ')':
                depth -= 1
                if depth == 0:
                    yield body[i + 1:j]
                    break


def main():
    # A primitive's C name maps to the MUF name through the NAMES macros, which
    # list implementations and names in the same order.
    levels = {}
    for module in MODULES:
        src = show(f"src/{module}.c")
        # Split into functions: "prim_name(PRIM_PROTOTYPE)\n{ ... }"
        for m in re.finditer(
                r'^(prim_\w+)\(PRIM_PROTOTYPE\)\s*\n\{(.*?)^\}',
                src, re.M | re.S):
            fname, body = m.group(1), m.group(2)
            # Only an *unconditional* floor counts. A check written as
            #   if ((mlev < 3) && !permissions(...)) abort
            # means "a wizard or the owner", not "level 3 or nothing", and
            # treating it as a floor would refuse the owner. Those are skipped
            # by looking for a permission test in the same condition.
            found = []
            for cond in conditions(body):
                if 'mlev' not in cond:
                    continue
                if re.search(r'permissions\s*\(|controls\s*\(|'
                             r'prop_read_perms|prop_write_perms|'
                             r'Wizard\s*\(|test_lock|already_created|'
                             r'Typeof\s*\(|FLAGS\s*\(', cond):
                    continue
                for lv in re.findall(r'mlev\s*<\s*(\w+)', cond):
                    if lv in LEVELS:
                        found.append(LEVELS[lv])
            if found:
                # The loosest unconditional floor is the level at which the
                # primitive is definitely callable.
                levels[fname] = min(found)

    # Map C function names to MUF primitive names through the FUNCS and NAMES
    # macros, which are parallel lists.
    name_of = {}
    for module in MODULES:
        header = show(f"include/{module}.h")
        funcs = re.search(r'#define\s+PRIMS_\w+_FUNCS\s+(.*?)(?=\n\s*\n|\n#define)',
                          header, re.S)
        names = re.search(r'#define\s+PRIMS_\w+_NAMES\s+(.*?)(?=\n\s*\n|\n#define)',
                          header, re.S)
        if not funcs or not names:
            continue
        flist = re.findall(r'\bprim_\w+', funcs.group(1).replace("\\\n", " "))
        nlist = re.findall(r'"((?:[^"\\]|\\.)*)"', names.group(1).replace("\\\n", " "))
        if len(flist) != len(nlist):
            print(f"  warning: {module} has {len(flist)} functions and "
                  f"{len(nlist)} names; skipped", file=sys.stderr)
            continue
        for fn, nm in zip(flist, nlist):
            name_of[fn] = nm

    table = {}
    for fn, lv in levels.items():
        nm = name_of.get(fn)
        if nm:
            table[nm] = lv

    q = json.dumps
    out = [
        "// Code generated from the mlev checks in Fuzzball 7's p_*.c.",
        "// DO NOT EDIT. Regenerate with: go generate ./internal/muf",
        "",
        "package muf",
        "",
        "// primMLevel is the mucker level a primitive requires.",
        "//",
        "// Upstream guards privileged primitives with \"if (mlev < N)\" inside",
        "// each implementation. Without these a program at mucker level 1 could",
        "// read passwords, change ownership and boot connections, so this is a",
        "// security surface rather than only a compatibility one.",
        "//",
        "// Where a primitive has several checks, the strictest is recorded: a",
        "// finer-grained gate would need the arguments, which the dispatcher",
        "// does not have.",
        "var primMLevel = map[string]int{",
    ]
    for nm in sorted(table):
        out.append(f"\t{q(nm)}: {table[nm]},")
    out += ["}", ""]

    OUT.write_text("\n".join(out) + "\n")
    subprocess.run(["gofmt", "-w", str(OUT)], check=True)
    print(f"{OUT.relative_to(ROOT)}: {len(table)} gated primitives "
          f"(from {len(levels)} functions with checks)")


if __name__ == "__main__":
    main()
