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
# A "mlev < N" sharing its condition with one of these is not a floor
# but a choice: "a wizard *or* whoever owns it". Recording it as a
# floor refuses the owner, which is the whole hazard this generator
# exists to avoid.
#
# This list grew by accretion -- "control_process(" was added after
# KILL came out as an unconditional 3 and broke a mucker-2 player
# killing their own program -- which is the argument for the two
# structural rules in guards() and _bare_abort() carrying most of the
# weight instead. A token list can only ever name the escape hatches
# somebody has already been bitten by.
ESCAPE_HATCH = re.compile(
    r'permissions\s*\(|controls\s*\(|control_process\s*\(|'
    r'prop_read_perms|prop_write_perms|'
    r'Wizard\s*\(|test_lock|already_created|'
    r'Typeof\s*\(|FLAGS\s*\(|'
    # "unless it is my own pid" -- GETPIDINFO's
    # "mlev < 3 && oper1->data.number != fr->pid" is the same shape as
    # an ownership escape hatch, against a running process instead of
    # an owned object.
    r'fr\s*->\s*pid|'
    # STATS/STATS_ARRAY spell the ownership test directly rather than
    # through permissions().
    r'OWNER\s*\(')

# Floors whose level is an @tune parameter rather than a literal.
#
# A map[string]int cannot express a gate a world can move at runtime,
# so each of these is checked inline by the primitive that has it --
# prim_misc2.go's USERLOG is the pattern, down to reproducing
# upstream's own oddly literal wording. They are named here so this
# generator can tell a known one from a new one, because the old code
# simply dropped any right-hand side it did not recognise: that is how
# ADDPENNIES, PENNIES and MOVEPENNIES came to have no gate at all
# while carrying an invented one of 4.
TUNABLE_FLOOR = {
    "tp_addpennies_muf_mlev",
    "tp_movepennies_muf_mlev",
    "tp_pennies_muf_mlev",
    "tp_userlog_mlev",
    "tp_mcp_muf_mlev",
}

# A floor the C does not have, kept deliberately because this
# server's implementation is not yet faithful enough to drop it.
#
# MOVETO's real gate is conditional: "if ((mlev < 3))" at p_db.c:234
# opens a block of extra mortal-only restrictions, on top of two
# genuinely conditional tests at :213 and :217, so this table should
# not hold it at all. But internal/muf's MOVETO is a bare
# h.MoveTo(what, dest) -- none of prim_moveto's type switch, no
# enter_room for a player, no parent_loop_check, no exit re-sourcing,
# no room reparenting, and none of its thirteen mlev-conditional
# refusals. Dropping the floor would hand a mucker-1 program an
# unvalidated raw move of any object in the database.
#
# So the floor stays until prim_moveto is ported, which needs five
# Host methods that do not exist yet. Recorded in
# docs/upstream-coverage.md, and this entry is the thing to delete
# when it lands -- not a name to add to.
HELD_FLOOR = {
    "MOVETO": 3,
}

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
    # src/p_props.c: both spell the requirement out as "Mucker level 3 or
    # greater required." rather than the generic "Permission denied." The
    # golden harness runs at mucker level 3 and so cannot reach either
    # abort; these two came from reading the C.
    "PARSEPROP", "PARSEPROPEX",
    # src/p_misc.c: SMTP_SEND's own "Permission Denied." — a capital D, and
    # no mention of the wizard bit.
    "SMTP_SEND",
}



def scrub(src):
    """Blank out comments and literal contents, keeping every offset.

    Everything below counts braces and parentheses, and a C string is
    full of both: abort_interp("Permission Denied (mlev < tp_x)") alone
    would unbalance the scan. Comments matter for a second reason --
    the escape-hatch test below looks for tokens like "permissions(",
    and a comment mentioning one would exempt a floor that is real.
    """
    out = list(src)
    i, n = 0, len(src)
    while i < n:
        c = src[i]
        if c == '/' and i + 1 < n and src[i + 1] == '/':
            while i < n and src[i] != '\n':
                out[i] = ' '
                i += 1
        elif c == '/' and i + 1 < n and src[i + 1] == '*':
            out[i] = out[i + 1] = ' '
            i += 2
            while i + 1 < n and not (src[i] == '*' and src[i + 1] == '/'):
                if src[i] != '\n':
                    out[i] = ' '
                i += 1
            if i + 1 < n:
                out[i] = out[i + 1] = ' '
                i += 2
        elif c in '"\'':
            quote = c
            i += 1
            while i < n and src[i] != quote:
                if src[i] == '\\':
                    out[i] = ' '
                    i += 1
                    if i < n:
                        out[i] = ' '
                        i += 1
                    continue
                out[i] = ' '
                i += 1
            i += 1
        else:
            i += 1
    return "".join(out)


_IF = re.compile(r'\bif\s*\(')


def _balanced(src, open_at, opener='(', closer=')'):
    """Index of the bracket matching the one at open_at, or -1."""
    depth = 0
    for k in range(open_at, len(src)):
        if src[k] == opener:
            depth += 1
        elif src[k] == closer:
            depth -= 1
            if depth == 0:
                return k
    return -1


def _bare_abort(src, pos):
    """Is the statement starting at pos nothing but abort_interp(...)?

    This is the first of the two structural rules. When the guarded
    statement is anything else -- nested ifs adding extra
    restrictions, or a prefix_message choosing how to word the output
    -- the "mlev <" test is selecting a *scope* and not refusing at
    all. Five of p_strings.c's and p_array.c's notify primitives are
    the second kind: their mlev<2 branch picks a message prefix and
    there is no abort anywhere on the path, so recording it as a floor
    stopped a mucker-1 program producing output at all.

    Both spellings have to be handled, and getting this wrong is not
    conservative in the safe direction. Most of p_props.c writes the
    braceless form

        if (mlev < 2)
            abort_interp("Permission denied.");

    and reading "the rest of the statement" as the rest of the
    function made every one of those look like a scope guard, which
    would have deleted eight real floors.
    """
    rest = src[pos:]
    body = rest.lstrip()
    if body.startswith('{'):
        start = pos + rest.index('{')
        end = _balanced(src, start, '{', '}')
        if end < 0:
            return False
        inner = src[start + 1:end].strip()
        m = re.match(r'abort_interp\s*\(', inner)
        if not m:
            return False
        close = _balanced(inner, m.end() - 1)
        if close < 0:
            return False
        # Nothing may follow it inside the block but its semicolon.
        return inner[close + 1:].strip().rstrip(';').strip() == ''
    m = re.match(r'abort_interp\s*\(', body)
    if not m:
        return False
    close = _balanced(body, m.end() - 1)
    if close < 0:
        return False
    # Braceless: the guarded statement ends at its own semicolon, and
    # whatever the function does next is none of our business.
    return body[close + 1:].lstrip().startswith(';')


def guards(body):
    """Yield (condition, depth, bare_abort) for each `if` in a body.

    depth is the brace nesting the `if` itself sits at, counted from
    the function body: an `if` written at the top level of the
    function is 0, and one inside an enclosing `if` or a switch is
    deeper. That is the second structural rule -- a deeper check is
    reachable only under a condition this extractor never sees, which
    is exactly how prim_setname's "if (mlev < 4)" nested inside
    "if (Typeof(ref) == TYPE_PLAYER)" was read as an absolute
    mucker-4 floor and refused a mortal renaming an object they own.

    Matching a condition to its first ")" is not enough either:
    "if ((mlev < 4) && !permissions(x))" would be cut after
    "(mlev < 4", hiding the permission test that makes it a choice.
    """
    depth = 0
    i, n = 0, len(body)
    while i < n:
        c = body[i]
        if c == '{':
            depth += 1
            i += 1
            continue
        if c == '}':
            depth -= 1
            i += 1
            continue
        m = _IF.match(body, i)
        if not m:
            i += 1
            continue
        open_at = m.end() - 1
        close = _balanced(body, open_at)
        if close < 0:
            i = open_at + 1
            continue
        yield (body[open_at + 1:close], depth,
               _bare_abort(body, close + 1))
        i = close + 1


def disjuncts(cond):
    """Split a condition on its top-level "||".

    Each disjunct aborts on its own, so one that names a level and has
    no escape hatch is a genuine floor even when another disjunct has
    one. prim_recycle is
    "(mlev < 3) || ((mlev < 4) && !permissions(ProgUID, result))":
    reading it whole, the "permissions(" exempts the lot and the
    unconditional floor of 3 is lost, which let a mucker-1 program
    recycle objects upstream refuses it.
    """
    parts, depth, start = [], 0, 0
    i = 0
    while i < len(cond):
        if cond[i] == '(':
            depth += 1
        elif cond[i] == ')':
            depth -= 1
        elif depth == 0 and cond.startswith('||', i):
            parts.append(cond[start:i])
            i += 2
            start = i
            continue
        i += 1
    parts.append(cond[start:])
    return parts


def main():
    # A primitive's C name maps to the MUF name through the NAMES macros, which
    # list implementations and names in the same order.
    levels = {}
    for module in MODULES:
        src = scrub(show(f"src/{module}.c"))
        # Split into functions: "prim_name(PRIM_PROTOTYPE)\n{ ... }".
        # The return type is normally on its own line, but prim_dump puts it
        # on the same one, so allow it either way — without the "void"
        # alternative that one primitive's floor goes unrecorded.
        for m in re.finditer(
                r'^(?:void\s+)?(prim_\w+)\(PRIM_PROTOTYPE\)\s*\n?\s*\{(.*?)^\}',
                src, re.M | re.S):
            fname, body = m.group(1), m.group(2)
            # Only an *unconditional* floor counts, and three things
            # can make a "mlev < N" look like one when it is not.
            #
            # An escape hatch in the same condition: written
            #   if ((mlev < 3) && !permissions(...)) abort
            # it means "a wizard or the owner", not "level 3 or
            # nothing", and recording it as a floor would refuse the
            # owner. Those are skipped by looking for a permission,
            # ownership, flag or type test alongside.
            #
            # The other two are structural and are guards() and
            # _bare_abort()'s business: a check nested inside an
            # enclosing condition, and a check whose body is not an
            # abort at all. Both used to be patched name by name --
            # CONDITIONAL_FLOOR held SETNAME, and the escape list grew
            # "control_process(" after KILL came out wrong -- and the
            # two rules together replace that list entirely.
            found = []
            for cond, depth, bare in guards(body):
                if 'mlev' not in cond:
                    continue
                if depth > 0 or not bare:
                    continue
                for part in disjuncts(cond):
                    if 'mlev' not in part:
                        continue
                    if re.search(ESCAPE_HATCH, part):
                        continue
                    for lv in re.findall(r'mlev\s*<\s*(\w+)', part):
                        if lv in LEVELS:
                            found.append(LEVELS[lv])
                        elif lv in TUNABLE_FLOOR:
                            # A runtime gate, checked inline by the
                            # primitive: a map[string]int cannot hold
                            # one. Listed so a *new* one is a failure
                            # rather than a silent drop.
                            continue
                        else:
                            raise SystemExit(
                                f"{module}.c: {fname} compares mlev "
                                f"against {lv!r}, which is neither a "
                                f"level nor a known tunable. Add it "
                                f"to LEVELS or TUNABLE_FLOOR -- do "
                                f"not let a floor go unrecorded.")
            if found:
                # The *loosest* unconditional floor is the level at
                # which the primitive is definitely callable: with two
                # of them one path works at the lower level, and
                # recording the higher would refuse that path here,
                # before the primitive could check for itself.
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

    table = dict(HELD_FLOOR)
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
        "// Only an *unconditional* floor is recorded. A check written",
        "// \"(mlev < 4) && !permissions(...)\" means \"a wizard or the",
        "// owner\", and so does one nested inside a type or flag test, or",
        "// one whose branch picks a message rather than refusing; a",
        "// finer-grained gate would need the arguments, which the",
        "// dispatcher does not have.",
        "//",
        "// One entry is not from the C at all: see HELD_FLOOR in",
        "// gen_mlev.py, which keeps MOVETO gated because this server's",
        "// implementation of it is not faithful enough to ungate.",
        "//",
        "// Where a primitive has several, the *loosest* is recorded: that",
        "// is the level at which it is definitely callable, and refusing",
        "// at the strictest would block a path that works before the",
        "// primitive could check for itself.",
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
