#!/usr/bin/env python3
"""
Helper script to generate encrypted access URLs for the Web SSH Bastion.
Reads the Fernet key from config.yaml (or --config / FERNET_KEY env var).
"""

from cryptography.fernet import Fernet
import base64
import os
import re
import sys
import argparse


def load_fernet_key(config_path):
    """Load fernet_key from config.yaml or FERNET_KEY environment variable."""
    env_key = os.environ.get("FERNET_KEY")
    if env_key:
        return env_key.encode()

    try:
        with open(config_path, "r") as f:
            content = f.read()
    except FileNotFoundError:
        print(f"Error: config file not found: {config_path}", file=sys.stderr)
        print("Pass --config PATH or set FERNET_KEY environment variable.", file=sys.stderr)
        sys.exit(1)

    match = re.search(r"fernet_key:\s*(\S+)", content)
    if not match or match.group(1) == "REPLACE_WITH_YOUR_OWN_KEY":
        print(f"Error: no valid fernet_key found in {config_path}", file=sys.stderr)
        print("Set security.fernet_key in config.yaml or use FERNET_KEY env var.", file=sys.stderr)
        sys.exit(1)

    return match.group(1).encode()


def generate_access_token(user, host, password=None, private_key_path=None, key=None):
    """Generate an encrypted access token."""
    f = Fernet(key)

    parts = [f"username={user}", f"hostname={host}"]

    if password:
        parts.append(f"password={password}")

    if private_key_path:
        with open(private_key_path, "rb") as key_file:
            private_key_content = key_file.read()
            private_key_b64 = base64.b64encode(private_key_content).decode()
            parts.append(f"privatekey={private_key_b64}")

    data = "&".join(parts)
    data_b64 = base64.b64encode(data.encode()).decode()
    return f.encrypt(data_b64.encode()).decode()


def generate_new_key():
    """Generate a new Fernet key."""
    return Fernet.generate_key().decode()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Generate encrypted access URLs for Web SSH Bastion")
    parser.add_argument("--generate-key", action="store_true", help="Generate a new Fernet key")
    parser.add_argument("--user", help="SSH username")
    parser.add_argument("--host", help="SSH host")
    parser.add_argument("--password", help="SSH password")
    parser.add_argument("--key", help="Path to private key file")
    parser.add_argument("--fernet-key", help="Fernet encryption key (overrides config and env)")
    parser.add_argument("--config", default="config.yaml", help="Path to gossh config.yaml")
    parser.add_argument("--base-url", default="http://localhost:8088", help="Base URL of the bastion server")

    args = parser.parse_args()

    if args.generate_key:
        print("New Fernet key:")
        print(generate_new_key())
        sys.exit(0)

    if not args.user or not args.host:
        parser.print_help()
        print("\nExample usage:")
        print("  python3 generate_url.py --user admin --host 192.168.1.100 --password mypass")
        print("  python3 generate_url.py --user admin --host 192.168.1.100 --key ~/.ssh/id_rsa")
        sys.exit(1)

    if args.fernet_key:
        fernet_key = args.fernet_key.encode()
    else:
        fernet_key = load_fernet_key(args.config)

    token = generate_access_token(args.user, args.host, args.password, args.key, fernet_key)
    url = f"{args.base_url}/?access={token}"

    print("Encrypted Access URL:")
    print(url)
    print("\nToken only:")
    print(token)
