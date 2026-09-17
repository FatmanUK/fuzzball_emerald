#!/usr/bin/env python3
"""Generate internal/tune/params_gen.go from Fuzzball 7's include/tunelist.h.

The upstream table is read straight out of git rather than vendored, so the
generated Go always reflects a specific upstream commit:

    python3 internal/tune/internal/gen/gen_params.py [git-ref]

Divergences from upstream (dropped, renamed and inert parameters) are declared
in the tables below and are the only hand-maintained part of the output.
"""
import json
import re
import subprocess
import sys
import pathlib

REF = sys.argv[1] if len(sys.argv) > 1 else "origin/mother"

# Anchored to the repo root so this works from anywhere, including the cwd
# `go generate ./internal/tune` uses.
ROOT = pathlib.Path(subprocess.run(
    ["git", "rev-parse", "--show-toplevel"],
    capture_output=True, text=True, check=True).stdout.strip())
OUT = ROOT / "internal/tune/params_gen.go"

# --- upstream divergences -------------------------------------------------

# Dropped: a TLS-only server cannot bootstrap its listeners from a database it
# has not opened yet, so all cert and protocol settings move to the environment.
DROPPED = {
    "ssl_cert_file":              "FBE_TLS_CERT_FILE",
    "ssl_key_file":               "FBE_TLS_KEY_FILE",
    "ssl_keyfile_passwd":         "FBE_TLS_KEY_PASSWORD",
    "ssl_min_protocol_version":   "FBE_TLS_MIN_VERSION",
    "ssl_cipher_preference_list": "FBE_TLS_CIPHER_POLICY",
    "ssl_auto_reload_certs":      "FBE_TLS_AUTO_RELOAD",
    "server_cipher_preference":   "FBE_TLS_CIPHER_POLICY",
    "starttls_allow":             "",   # meaningless: every listener is TLS
}

# Renamed. The old spelling still resolves via Param.LegacyName, because MUF
# looks parameters up by string at runtime.
RENAMED = {"smtp_ssl_type": "smtp_tls_mode"}

# Inert: kept so MUF `sysparm` lookups keep working, but no longer steering
# anything, because write-behind persistence replaced dump-and-pause.
INERT = {
    "dump_interval":     "superseded by continuous write-behind; see flush_interval",
    "dump_warntime":     "no dump pause to warn about",
    "dumpwarn_mesg":     "no dump pause to warn about",
    "dumping_mesg":      "saving never blocks the world",
    "dumpdone_mesg":     "saving never blocks the world",
    "dumpdone_warning":  "saving never blocks the world",
    "dbdump_warning":    "saving never blocks the world",
    "diskbase_propvals": "DISKBASE is gone; properties live in Postgres",
}

UPSTREAM_COUNT = 169

# --- parsing --------------------------------------------------------------

MLEV = {"0": 0, "MLEV_APPRENTICE": 1, "MLEV_JOURNEYMAN": 2, "MLEV_MASTER": 3,
        "MLEV_WIZARD": 4, "MLEV_GOD": 4}
TYPEMAP = {"TP_TYPE_STRING": "String", "TP_TYPE_TIMESPAN": "Timespan",
           "TP_TYPE_INTEGER": "Integer", "TP_TYPE_DBREF": "Dbref",
           "TP_TYPE_BOOLEAN": "Boolean"}
GOTYPE = {"String": "TypeString", "Timespan": "TypeTimespan",
          "Integer": "TypeInteger", "Dbref": "TypeDbref",
          "Boolean": "TypeBoolean"}
OBJTYPE = {"TYPE_ROOM": "TypeRoom", "TYPE_THING": "TypeThing",
           "TYPE_EXIT": "TypeExit", "TYPE_PLAYER": "TypePlayer",
           "TYPE_PROGRAM": "TypeProgram", "TYPE_GARBAGE": "TypeGarbage",
           "NOTYPE": "NoType"}
REFCONST = {"GLOBAL_ENVIRONMENT": "ref.GlobalEnvironment", "GOD": "ref.God",
            "NOTHING": "ref.Nothing", "HOME": "ref.Home", "NIL": "ref.Nil"}


def blocks(text):
    """Yield the body of each brace-delimited entry in tune_list[]."""
    cur, depth = [], 0
    for line in text.splitlines():
        stripped = line.strip()
        if stripped == "{" and depth == 0:
            depth, cur = 1, []
        elif stripped in ("},", "}") and depth == 1:
            yield "\n".join(cur)
            depth = 0
        elif depth == 1:
            cur.append(line)


def fields(block):
    """Split a C initializer body on top-level commas, respecting strings."""
    parts, buf, i, instr = [], [], 0, False
    while i < len(block):
        c = block[i]
        if instr:
            buf.append(c)
            if c == "\\":
                buf.append(block[i + 1]); i += 2; continue
            if c == '"':
                instr = False
            i += 1
        elif c == '"':
            instr = True; buf.append(c); i += 1
        elif c == ",":
            parts.append("".join(buf).strip()); buf = []; i += 1
        else:
            buf.append(c); i += 1
    if "".join(buf).strip():
        parts.append("".join(buf).strip())
    return [p for p in parts if p]


def c_string(lit):
    """Join adjacent C string literals and decode their escapes."""
    pieces = re.findall(r'"((?:[^"\\]|\\.)*)"', lit)
    if not pieces:
        raise ValueError(f"not a string literal: {lit!r}")
    esc = {"n": "\n", "r": "\r", "t": "\t", "\\": "\\", '"': '"', "0": "\0"}
    out = []
    for p in pieces:
        i = 0
        while i < len(p):
            if p[i] == "\\" and i + 1 < len(p):
                out.append(esc.get(p[i + 1], p[i + 1])); i += 2
            else:
                out.append(p[i]); i += 1
    return "".join(out)


def parse(text):
    entries = []
    for b in blocks(text):
        f = fields(b)
        assert f[5].startswith(".defaultval."), f[:6]
        assert f[6].startswith(".currentval."), f[:7]
        rest = f[7:]
        entries.append(dict(
            name=c_string(f[0]), label=c_string(f[1]),
            group=c_string(f[2]), module=c_string(f[3]),
            type=TYPEMAP[f[4]], default=f[5].split("=", 1)[1].strip(),
            readmlev=MLEV[rest[0]], writemlev=MLEV[rest[1]],
            god_only=(rest[1] == "MLEV_GOD"),
            # rest[2] is isdefault, always true in the source table
            nullable=len(rest) > 3 and rest[3] == "true",
            objtype=OBJTYPE[rest[4]] if len(rest) > 4 else None,
        ))
    return entries


def go_default(e):
    t, d = e["type"], e["default"]
    if t == "String":
        return f"Value{{Str: {json.dumps(c_string(d))}}}"
    if t == "Boolean":
        assert d in ("true", "false"), (e["name"], d)
        return f"Value{{Bool: {d}}}"
    if t == "Integer":
        return f"Value{{Num: {int(d, 0)}}}"
    if t == "Timespan":
        return f"Value{{Span: {int(d, 0)} * time.Second}}"
    if t == "Dbref":
        return f"Value{{Ref: {REFCONST[d]}}}"
    raise AssertionError(t)


def main():
    header = subprocess.run(["git", "show", f"{REF}:include/tunelist.h"],
                            capture_output=True, text=True, check=True,
                            cwd=ROOT).stdout
    body = header[header.index("struct tune_entry tune_list[] = {"):]
    entries = parse(body)
    assert len(entries) == UPSTREAM_COUNT, \
        f"parsed {len(entries)} parameters, expected {UPSTREAM_COUNT}"
    for name in list(DROPPED) + list(RENAMED) + list(INERT):
        assert any(e["name"] == name for e in entries), \
            f"{name} is not an upstream parameter"

    q = json.dumps  # JSON escaping is valid Go for these strings
    out = [
        "// Code generated from Fuzzball 7 include/tunelist.h. DO NOT EDIT.",
        "// Regenerate with: go generate ./internal/tune",
        "",
        "package tune",
        "",
        "import (",
        '\t"time"',
        "",
        '\t"github.com/FatmanUK/fuzzball_emerald/internal/ref"',
        ")",
        "",
        "// params is the full parameter table, ordered by name.",
        "var params = []Param{",
    ]
    kept = 0
    for e in sorted(entries, key=lambda x: x["name"]):
        name = e["name"]
        if name in DROPPED:
            continue
        kept += 1
        out.append("\t{")
        out.append(f"\t\tName:  {q(RENAMED.get(name, name))},")
        out.append(f"\t\tLabel: {q(e['label'])},")
        out.append(f"\t\tGroup: {q(e['group'])},")
        if e["module"]:
            out.append(f"\t\tModule: {q(e['module'])},")
        out.append(f"\t\tType:  {GOTYPE[e['type']]},")
        out.append(f"\t\tDefault: {go_default(e)},")
        out.append(f"\t\tReadMLev:  {e['readmlev']},")
        out.append(f"\t\tWriteMLev: {e['writemlev']},")
        if e["god_only"]:
            out.append("\t\tGodOnly:   true,")
        if e["nullable"]:
            out.append("\t\tNullable:  true,")
        if e["objtype"]:
            out.append(f"\t\tObjType:   ref.{e['objtype']},")
            out.append("\t\tHasObjType: true,")
        if name in RENAMED:
            out.append(f"\t\tLegacyName: {q(name)},")
        if name in INERT:
            out.append(f"\t\tInert: {q(INERT[name])},")
        out.append("\t},")
    out += [
        "}",
        "",
        "// droppedParams names Fuzzball 7 parameters that Emerald does not implement,",
        "// mapped to the environment variable that replaced each. They are recognised",
        "// only so the importer and @tune can explain themselves instead of failing",
        '// with an unhelpful "unknown parameter".',
        "var droppedParams = map[string]string{",
    ]
    for k in sorted(DROPPED):
        out.append(f"\t{q(k)}: {q(DROPPED[k])},")
    out.append("}")

    OUT.write_text("\n".join(out) + "\n")
    subprocess.run(["gofmt", "-w", str(OUT)], check=True)
    print(f"{OUT.relative_to(ROOT)}: {kept} parameters "
          f"({len(entries)} upstream, {len(DROPPED)} dropped, "
          f"{len(RENAMED)} renamed, {len(INERT)} inert)")


if __name__ == "__main__":
    main()
