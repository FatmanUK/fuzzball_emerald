#!/usr/bin/env python3
"""Generate internal/muf/mlev_gen.go: each primitive's mucker-level floor.

Fuzzball guards privileged primitives with "if (mlev < N) abort_interp(...)"
inside each implementation. Those checks are a security surface, not just a
compatibility one, so they are extracted rather than transcribed.

    python3 internal/muf/internal/gen/gen_mlev.py [path/to/fuzzball]
"""
import json
import re
import subprocess
import sys
import pathlib


ROOT = pathlib.Path(subprocess.run(
    ["git", "rev-parse", "--show-toplevel"],
    capture_output=True, text=True, check=True).stdout.strip())
OUT = ROOT / "internal/muf/mlev_gen.go"

# The upstream C is vendored as a submodule at fuzzball/. Pass a path to read
# a different checkout instead.
SRC = pathlib.Path(sys.argv[1]) if len(sys.argv) > 1 else ROOT / "fuzzball"


def show(path):
    """Read one file out of the upstream checkout."""
    p = SRC / path
    if not p.exists():
        raise SystemExit(
            f"{p} not found; run 'git submodule update --init fuzzball'")
    return p.read_text()

MODULES = ["p_array", "p_connects", "p_db", "p_error", "p_float", "p_math",
           "p_mcp", "p_misc", "p_props", "p_regex", "p_stack", "p_strings"]

# Symbolic levels used in the checks.
LEVELS = {"MLEV_APPRENTICE": 1, "MLEV_JOURNEYMAN": 2, "MLEV_MASTER": 3,
          "MLEV_WIZARD": 4, "MLEV_GOD": 4,
          "1": 1, "2": 2, "3": 3, "4": 4}

# This table only records a floor's *level*, not its abort message, so the
# dispatcher (internal/muf/prim.go's primitive()) always prints one of two
# generic messages by level — "Permission denied." or, for level 4,
# "Permission denied.  Requires Wizbit." That second wording matches most of
# p_db.c's own level-4 checks, verified via golden, but not every module:
# p_misc.c's FORCE, FORCEDBY and FORCEDBY_ARRAY all abort with "Wizbit only
# primitive." instead, discovered via golden when FORCE was ported, and
# p_connects.c has a third variant, "Requires Wizbit.", not yet hit by a
# ported primitive. Names here are excluded from the table entirely and
# implement their own mlev check inline with the exact right wording — see
# prim_proc.go's FORCE/FORCEDBY/FORCEDBY_ARRAY — rather than teaching this
# generator to track messages for what is so far three known exceptions.
#
# Below level 4, the equivalent generic message is a bare "Permission
# denied." — also not universal: GETPIDS (src/p_db.c) aborts with
# "Permission denied.  Requires Mucker Level 3.", found the same way, when
# it was ported.
CUSTOM_ABORT_MESSAGE = {
    "FORCE", "FORCEDBY", "FORCEDBY_ARRAY", "GETPIDS", "WATCHPID",
    # src/p_connects.c: every mlev floor in this module has its own wording,
    # never the dispatcher's generic "Permission denied." or "Permission
    # denied.  Requires Wizbit." — three distinct level-3 variants alone
    # ("Mucker level 3 primitive.", "Requires Mucker Level 3.", "Requires
    # Mucker Level 3 or better.") plus two level-4 variants ("Primitive is a
    # wizbit only command.", "Requires Wizbit."). See prim_connects.go.
    "ONLINE", "ONLINE_ARRAY", "DESCRDBREF", "DESCRSECURE?",
    "DESCRIDLE", "DESCRLEASTIDLE", "DESCRMOSTIDLE", "DESCRTIME",
    "DESCRHOST", "DESCRUSER", "DESCRBOOT", "DESCRNOTIFY", "NEXTDESCR",
    "DESCRIPTORS", "DESCR_ARRAY", "DESCR_SETUSER", "DESCRFLUSH",
    "FIRSTDESCR", "LASTDESCR", "DESCRBUFSIZE", "SETWIDTH", "SETHEIGHT",
    # src/p_misc.c: SETSYSPARM's own "Wizbit only primitive." — see
    # prim_sysparm.go. SYSPARM/SYSPARM_ARRAY have no fixed floor of their
    # own at all; their gate is per-parameter (TUNE_MLEV(player)).
    "SETSYSPARM",
    # src/p_misc.c: IGNORING?/IGNORE_ADD/IGNORE_DEL's own capitalised
    # "Permission Denied." — see prim_misc2.go.
    "IGNORING?", "IGNORE_ADD", "IGNORE_DEL",
    # src/p_array.c: ARRAY_NOTIFY_SECURE's own "Mucker level 3 primitive.",
    # the same non-generic wording as most of p_connects.c.
    "ARRAY_NOTIFY_SECURE",
    # src/p_props.c: plain "Permission denied." at mlev 4, not the generic
    # dispatcher's "Permission denied.  Requires Wizbit."
    "BLESSPROP", "UNBLESSPROP", "PARSEMPIBLESSED",
    # src/p_db.c: NEXTENTRANCE's own "Permission denied.  Requires Mucker
    # Level 3." — the generic dispatcher's level<4 wording has no such
    # suffix at all.
    "NEXTENTRANCE",
    # src/p_db.c: FINDNEXT's own "Permission denied.  Requires at least
    # Mucker Level 2.", plus two further level-3 messages that depend on
    # which owner was asked for — see prim_findflags.go.
    "FINDNEXT",
    # src/p_misc.c: EVENT_SEND's own "Requires Mucker level 3 or better."
    # — lowercase "level", and no "Permission denied." at all.
    "EVENT_SEND",
}



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
        # Split into functions: "prim_name(PRIM_PROTOTYPE)\n{ ... }".
        # The return type is normally on its own line, but prim_dump puts it
        # on the same one, so allow it either way — without the "void"
        # alternative that one primitive's floor goes unrecorded.
        for m in re.finditer(
                r'^(?:void\s+)?(prim_\w+)\(PRIM_PROTOTYPE\)\s*\n?\s*\{(.*?)^\}',
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
                             r'control_process\s*\(|'
                             r'prop_read_perms|prop_write_perms|'
                             r'Wizard\s*\(|test_lock|already_created|'
                             r'Typeof\s*\(|FLAGS\s*\(|'
                             # "unless it's my own pid" — GETPIDINFO's own
                             # "mlev < 3 && oper1->data.number != fr->pid"
                             # is this same shape as an ownership escape
                             # hatch, just against a running process instead
                             # of an owned object; found the same way KILL's
                             # control_process gap was, by checking the C
                             # once a primitive using this table read wrong.
                             r'fr\s*->\s*pid|'
                             # STATS/STATS_ARRAY's own "mlev < 3 &&
                             # OWNER(ref) != player" — the same ownership
                             # escape hatch as "permissions(...)", just
                             # spelled directly instead of through that
                             # helper.
                             r'OWNER\s*\(', cond):
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
        if nm and nm not in CUSTOM_ABORT_MESSAGE:
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
