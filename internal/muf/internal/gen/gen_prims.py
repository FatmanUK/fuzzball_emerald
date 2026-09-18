#!/usr/bin/env python3
"""Generate internal/muf/prims_gen.go: the MUF primitive name table.

The order matters. Fuzzball's get_primitive returns a token's index in
base_inst[] plus one, and the compiler emits that number, so the table is
reproduced in the same order the C builds it.

    python3 internal/muf/internal/gen/gen_prims.py [git-ref]
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
OUT = ROOT / "internal/muf/prims_gen.go"
OUT_DEFS = ROOT / "internal/muf/defs_gen.go"


def show(path):
    return subprocess.run(["git", "show", f"{REF}:{path}"],
                          capture_output=True, text=True, check=True,
                          cwd=ROOT).stdout


def macro_names(header, macro):
    """Extract the string literals from a PRIMS_*_NAMES macro definition."""
    text = show(header)
    m = re.search(rf"#define\s+{macro}\s+(.*?)(?=\n\s*\n|\n#)", text, re.S)
    if not m:
        raise SystemExit(f"{macro} not found in {header}")
    body = m.group(1).replace("\\\n", " ")
    return re.findall(r'"((?:[^"\\]|\\.)*)"', body)


# The order base_inst[] builds them in, from src/compile.c.
GROUPS = [
    ("include/p_connects.h", "PRIMS_CONNECTS_NAMES"),
    ("include/p_db.h",       "PRIMS_DB_NAMES"),
    ("include/p_math.h",     "PRIMS_MATH_NAMES"),
    ("include/p_misc.h",     "PRIMS_MISC_NAMES"),
    ("include/p_props.h",    "PRIMS_PROPS_NAMES"),
    ("include/p_stack.h",    "PRIMS_STACK_NAMES"),
    ("include/p_strings.h",  "PRIMS_STRINGS_NAMES"),
    ("include/p_array.h",    "PRIMS_ARRAY_NAMES"),
    ("include/p_float.h",    "PRIMS_FLOAT_NAMES"),
    ("include/p_error.h",    "PRIMS_ERROR_NAMES"),
    ("include/p_mcp.h",      "PRIMS_MCP_NAMES"),
    # MCPGUI_SUPPORT is defined in include/config.h, so these are present.
    ("include/p_mcpgui.h",   "PRIMS_MCPGUI_NAMES"),
    ("include/p_regex.h",    "PRIMS_REGEX_NAMES"),
    ("include/p_stack.h",    "PRIMS_INTERNAL_NAMES"),
]

# The nine base instructions, which are control flow rather than primitives
# proper but share the numbering.
BASE = ["JMP", "READ", "SLEEP", "CALL", "EXECUTE", "EXIT", "EVENT_WAITFOR",
        "CATCH", "CATCH_DETAILED"]


def main():
    names = list(BASE)
    per_group = []
    for header, macro in GROUPS:
        got = macro_names(header, macro)
        per_group.append((macro, len(got)))
        names.extend(got)

    seen = {}
    for i, n in enumerate(names):
        if n in seen:
            raise SystemExit(f"duplicate primitive {n!r} at {i} and {seen[n]}")
        seen[n] = i

    q = json.dumps
    out = [
        "// Code generated from Fuzzball 7's primitive tables. DO NOT EDIT.",
        "// Regenerate with: go generate ./internal/muf",
        "",
        "package muf",
        "",
        "// primNames lists every primitive the compiler recognises, in the order",
        "// Fuzzball's base_inst[] builds them. A token's primitive number is its",
        "// index here plus one, matching get_primitive, so a name's position is",
        "// part of the contract and must not be reordered.",
        "//",
        "// Names beginning with a space are internal: the tokenizer splits on",
        "// whitespace, so no program can name them. The compiler emits them for",
        "// FOR, FOREACH and TRY.",
        "var primNames = []string{",
    ]
    for n in names:
        out.append(f"\t{q(n)},")
    out += ["}", ""]

    OUT.write_text("\n".join(out) + "\n")
    subprocess.run(["gofmt", "-w", str(OUT)], check=True)
    print(f"{OUT.relative_to(ROOT)}: {len(names)} names "
          f"({len(BASE)} base + {len(names) - len(BASE)} primitives)")
    for macro, n in per_group:
        print(f"  {macro:28} {n}")


def c_string(lit):
    """Join adjacent C string literals and decode their escapes."""
    pieces = re.findall(r'"((?:[^"\\]|\\.)*)"', lit)
    esc = {"n": "\n", "r": "\r", "t": "\t", "\\": "\\", '"': '"'}
    out = []
    for piece in pieces:
        i = 0
        while i < len(piece):
            if piece[i] == "\\" and i + 1 < len(piece):
                out.append(esc.get(piece[i + 1], piece[i + 1]))
                i += 2
            else:
                out.append(piece[i])
                i += 1
    return "".join(out)


def gen_defines():
    """Extract include_internal_defs' table of built-in $defines."""
    src = show("src/compile.c")
    body = src[src.index("include_internal_defs(COMPSTATE * cstat)"):]
    body = body[:body.index("\ninit_defs")]

    # Resolve the MESGPROP_* and SORTTYPE_* constants the table refers to.
    consts = {}
    for header in ("include/db.h", "include/array.h", "include/interp.h",
                   "include/p_array.h"):
        for name, val in re.findall(
                r'#define\s+(MESGPROP_\w+|SORTTYPE_\w+)\s+(.+)',
                show(header)):
            # Strip a trailing doc comment. Splitting on "/" would cut the
            # value itself, since property paths contain slashes.
            val = re.sub(r'\s*/\*.*$', '', val)
            consts[name] = val.strip()

    def resolve(expr):
        # Substitute repeatedly: the sort-type constants are defined in terms
        # of each other.
        expr = expr.strip()
        for _ in range(8):
            before = expr
            for name, val in consts.items():
                expr = re.sub(r'\b' + re.escape(name) + r'\b', val, expr)
            if expr == before:
                break
        return expr

    def as_int(expr):
        """Evaluate a small C integer expression: literals, parens and '|'."""
        expr = expr.strip()
        if not re.fullmatch(r'[0-9a-fA-FxX\s()|]+', expr):
            return None
        total = 0
        for part in expr.replace("(", " ").replace(")", " ").split("|"):
            part = part.strip()
            if not part:
                return None
            try:
                total |= int(part, 0)
            except ValueError:
                return None
        return total

    defs = []
    # insert_def(cstat, "name", "value") - the value may span lines and
    # concatenate several literals.
    for m in re.finditer(
            r'insert_def\(cstat,\s*"((?:[^"\\]|\\.)*)",\s*(.*?)\);',
            body, re.S):
        name, value = m.group(1), resolve(m.group(2))
        # Skip the ones whose value is a C variable rather than a literal.
        if '"' not in value:
            continue
        defs.append((c_string('"' + name + '"'), c_string(value)))

    for m in re.finditer(
            r'insert_intdef\(cstat,\s*"((?:[^"\\]|\\.)*)",\s*([^)]+)\);',
            body):
        name = m.group(1)
        value = as_int(resolve(m.group(2)))
        if value is None:
            continue
        defs.append((c_string('"' + name + '"'), str(value)))

    q = json.dumps
    out = [
        "// Code generated from include_internal_defs in Fuzzball 7's",
        "// src/compile.c. DO NOT EDIT.",
        "// Regenerate with: go generate ./internal/muf",
        "",
        "package muf",
        "",
        "// BuiltinDefines are the $define substitutions the compiler installs",
        "// before reading a program. They are why \"}tell\", \"[]\", \"desc\" and the",
        "// case/when/end/default/endcase idiom work without being primitives.",
        "//",
        "// Definitions whose value is a runtime variable rather than a literal",
        "// are omitted: __version and __muckname depend on the server, and the",
        "// caller supplies them.",
        "var BuiltinDefines = map[string]string{",
    ]
    for name, value in sorted(defs):
        out.append(f"\t{q(name)}: {q(value)},")
    out += ["}", ""]

    OUT_DEFS.write_text("\n".join(out) + "\n")
    subprocess.run(["gofmt", "-w", str(OUT_DEFS)], check=True)
    print(f"{OUT_DEFS.relative_to(ROOT)}: {len(defs)} built-in defines")


if __name__ == "__main__":
    main()
    gen_defines()
