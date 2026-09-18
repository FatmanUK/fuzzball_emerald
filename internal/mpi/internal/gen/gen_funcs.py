#!/usr/bin/env python3
"""Generate internal/mpi/funcs_gen.go: the MPI function table.

Each entry carries the arity and the flags that decide how its arguments are
handled, which the parser needs before it can call anything.

    python3 internal/mpi/internal/gen/gen_funcs.py [git-ref]
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
OUT = ROOT / "internal/mpi/funcs_gen.go"


def main():
    header = subprocess.run(["git", "show", f"{REF}:include/mfun.h"],
                            capture_output=True, text=True, check=True,
                            cwd=ROOT).stdout
    body = header[header.index("static struct mfun_dat mfun_list[] = {"):]
    body = body[:body.index("\n};")]

    entries = []
    for m in re.finditer(
            r'\{"([A-Z0-9_?!*+/<>=-]+)",\s*(\w+),\s*(-?\d+),\s*(-?\d+),'
            r'\s*(-?\d+),\s*(-?\d+),\s*(-?\d+)\}', body):
        name, fn, parsep, postp, stripp, minargs, maxargs = m.groups()
        entries.append(dict(
            name=name, fn=fn,
            parse=parsep != "0", postparse=postp != "0", strip=stripp != "0",
            min=int(minargs), max=int(maxargs)))

    seen = set()
    for e in entries:
        if e["name"] in seen:
            raise SystemExit(f"duplicate MPI function {e['name']}")
        seen.add(e["name"])

    q = json.dumps
    out = [
        "// Code generated from mfun_list in Fuzzball 7's include/mfun.h.",
        "// DO NOT EDIT. Regenerate with: go generate ./internal/mpi",
        "",
        "package mpi",
        "",
        "// functions describes every MPI function the parser recognises.",
        "//",
        "// The flags decide how a call is handled before its implementation",
        "// ever runs, so they are part of the parser rather than of any one",
        "// function: Parse says the arguments are evaluated first, which",
        "// {and} and {if} switch off so they can stop early; Strip trims",
        "// spaces from them; and PostParse evaluates what comes back.",
        "var functions = map[string]*Func{",
    ]
    for e in sorted(entries, key=lambda x: x["name"]):
        parts = [f'Name: {q(e["name"])}']
        if e["parse"]:
            parts.append("Parse: true")
        if e["postparse"]:
            parts.append("PostParse: true")
        if e["strip"]:
            parts.append("Strip: true")
        parts.append(f'Min: {e["min"]}')
        parts.append(f'Max: {e["max"]}')
        out.append(f'\t{q(e["name"])}: {{{", ".join(parts)}}},')
    out += ["}", ""]

    OUT.write_text("\n".join(out) + "\n")
    subprocess.run(["gofmt", "-w", str(OUT)], check=True)
    print(f"{OUT.relative_to(ROOT)}: {len(entries)} MPI functions")


if __name__ == "__main__":
    main()
