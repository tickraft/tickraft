#!/usr/bin/env python3
"""Merge a contributor's login and emails into the CLA signer registry.

Usage:
    SIGNER=<login> EMAILS=<email1>,<email2> python3 merge-signer.py

Reads .github/CLA/signers.json, adds the signer (idempotent), and writes the
file back only when something changed. Also writes ``CHANGED=true|false`` to
the ``GITHUB_ENV`` file so subsequent GitHub Actions steps can skip redundant
work when the signer was already in the registry.
"""

import json
import os
import sys

PATH = ".github/CLA/signers.json"


def main() -> int:
    login = os.environ.get("SIGNER", "")
    if not login:
        print("SIGNER environment variable is required", file=sys.stderr)
        return 1
    emails = [e for e in os.environ.get("EMAILS", "").split(",") if e]

    with open(PATH) as f:
        data = json.load(f)

    changed = False
    users = [str(u).lower() for u in data.get("users", [])]
    if login.lower() not in users:
        data.setdefault("users", []).append(login)
        changed = True
        print(f"recorded signature for {login}")
    else:
        print(f"{login} already signed")

    existing_emails = [str(e).lower() for e in data.get("emails", [])]
    for e in emails:
        if e not in existing_emails:
            data.setdefault("emails", []).append(e)
            existing_emails.append(e)
            changed = True
            print(f"recorded email {e} for {login}")

    if changed:
        with open(PATH, "w") as f:
            json.dump(data, f, indent=2)
            f.write("\n")

    # Emit CHANGED to GITHUB_ENV so subsequent workflow steps can decide
    # whether to post a confirmation comment / re-trigger CI.
    github_env = os.environ.get("GITHUB_ENV")
    if github_env:
        with open(github_env, "a") as envf:
            envf.write(f"CHANGED={str(changed).lower()}\n")

    return 0


if __name__ == "__main__":
    sys.exit(main())
