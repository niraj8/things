#!/bin/bash

# domain-generator.sh
# Generate domain combinations for a given TLD and length
# Character set: a-z, 0-9

set -euo pipefail

# Check arguments
if [[ $# -ne 2 ]]; then
    echo "Usage: $0 <tld> <num_chars>"
    echo "Examples:"
    echo "  $0 com 2    # Generate 2-char .com domains"
    echo "  $0 xyz 3    # Generate 3-char .xyz domains"
    exit 1
fi

tld="$1"
length="$2"

# Validate length is a number
if ! [[ "$length" =~ ^[1-9][0-9]*$ ]]; then
    echo "Error: Length must be a positive integer" >&2
    exit 1
fi

# Generate combinations using printf and brace expansion
case $length in
    1)
        printf '%s\n' {a..z} {0..9} | sed "s/$/.$tld/"
        ;;
    2)
        printf '%s\n' {a..z,0..9}{a..z,0..9} | sed "s/$/.$tld/"
        ;;
    3)
        printf '%s\n' {a..z,0..9}{a..z,0..9}{a..z,0..9} | sed "s/$/.$tld/"
        ;;
    4)
        printf '%s\n' {a..z,0..9}{a..z,0..9}{a..z,0..9}{a..z,0..9} | sed "s/$/.$tld/"
        ;;
    5)
        printf '%s\n' {a..z,0..9}{a..z,0..9}{a..z,0..9}{a..z,0..9}{a..z,0..9} | sed "s/$/.$tld/"
        ;;
    *)
        echo "Error: Length must be between 1 and 5" >&2
        exit 1
        ;;
esac