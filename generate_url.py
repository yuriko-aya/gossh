#!/usr/bin/env python3
"""
Helper script to generate encrypted access URLs for the Web SSH Bastion
"""

from cryptography.fernet import Fernet
import base64
import sys
import argparse

# Default key - should match the one in main.go
DEFAULT_KEY = b'boFzsBC8_fuLeMR2JM75_ZyeQEcm_simjV81EURjxew='

def generate_access_token(user, host, private_key_path=None, key=DEFAULT_KEY):
    """Generate an encrypted access token"""
    f = Fernet(key)
    
    # Build credential string
    parts = [f"username={user}", f"hostname={host}"]
    
    # Add private key if provided
    if private_key_path:
        with open(private_key_path, 'rb') as key_file:
            private_key_content = key_file.read()
            private_key_b64 = base64.b64encode(private_key_content).decode()
            parts.append(f"privatekey={private_key_b64}")
    
    data = "&".join(parts)
    data_b64 = base64.b64encode(data.encode()).decode()
    
    # Encrypt
    token = f.encrypt(data_b64.encode()).decode()
    
    return token

def generate_new_key():
    """Generate a new Fernet key"""
    return Fernet.generate_key().decode()

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description='Generate encrypted access URLs for Web SSH Bastion')
    parser.add_argument('--generate-key', action='store_true', help='Generate a new Fernet key')
    parser.add_argument('--user', help='SSH username')
    parser.add_argument('--host', help='SSH host')
    parser.add_argument('--key', help='Path to private key file')
    parser.add_argument('--fernet-key', help='Custom Fernet encryption key')
    parser.add_argument('--base-url', default='http://localhost:8088', help='Base URL of the bastion server')
    
    args = parser.parse_args()
    
    if args.generate_key:
        print("New Fernet key:")
        print(generate_new_key())
        sys.exit(0)
    
    if not args.user or not args.host:
        parser.print_help()
        print("\nExample usage:")
        print("  python3 generate_url.py --user root --host example.com --key ~/.ssh/id_rsa")
        sys.exit(1)
    
    fernet_key = args.fernet_key.encode() if args.fernet_key else DEFAULT_KEY
    
    token = generate_access_token(args.user, args.host, args.key, fernet_key)
    url = f"{args.base_url}/?access={token}"
    
    print("Encrypted Access URL:")
    print(url)
    print("\nToken only:")
    print(token)
