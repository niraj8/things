#!/bin/bash

# Check if gh CLI is installed
if ! command -v gh &> /dev/null
then
    echo "gh CLI not found. Please install it from https://cli.github.com/"
    exit 1
fi

# List of GitHub repository URLs
repos=(
    "https://github.com/callstack/react-native-paper"
    "https://github.com/wix/react-native-ui-lib"
    "https://github.com/react-native-elements/react-native-elements"
    "https://github.com/microsoft/fluentui-react-native"
    "https://github.com/tamagui/tamagui"
    "https://github.com/nativewind/nativewind"
    "https://github.com/gluestack/gluestack-ui"
    "https://github.com/mrzachnugent/react-native-reusables"
    "https://github.com/akveo/react-native-ui-kitten"
)

# Function to extract the owner and repo name from the URL
get_repo_info() {
    local url=$1
    local owner=$(echo $url | cut -d'/' -f4)
    local repo=$(echo $url | cut -d'/' -f5)
    echo "$owner/$repo"
}

# Fetch stars for each repository
for url in "${repos[@]}"; do
    repo_info=$(get_repo_info $url)
    stars=$(gh repo view $repo_info --json stargazerCount --jq '.stargazerCount')
    if [ $? -eq 0 ]; then
        echo "Repository: $repo_info - Stars: $stars"
    else
        echo "Failed to fetch stars for $repo_info"
    fi
done
