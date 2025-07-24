#!/bin/bash

# 1. Ask for and validate the release branch
while true; do
    read -rp "Enter release branch (e.g., release-1.15): " RELEASE_BRANCH
    if [[ "$RELEASE_BRANCH" =~ ^release-.* ]]; then
        break
    else
        echo "Error: Branch must start with 'release-'. Please try again."
    fi
done

# 2. Ask for tag type
while true; do
    read -rp "What kind of tag is this? (rc/beta/release): " TAG_TYPE
    TAG_TYPE=$(echo "$TAG_TYPE" | tr '[:upper:]' '[:lower:]') # normalize to lowercase
    
    if [[ "$TAG_TYPE" == "rc" || "$TAG_TYPE" == "beta" || "$TAG_TYPE" == "release" ]]; then
        break
    else
        echo "Error: Invalid tag type. Must be 'rc', 'beta', or 'release'. Please try again."
    fi
done

# Ask for tag input
## TODO: compute tag automatically based on the type of tag from past tags
while true; do
    read -rp "Enter tag (e.g., v1.15.0-rc.1): " NEW_TAG
    if [[ "$NEW_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-.*)?$ ]]; then
        break
    else
        echo "Error: Tag must follow format v<major>.<minor>.<patch>[-suffix] (e.g., v1.15.0 or v1.15.0-rc.1). Please try again."
    fi
done

# 3. Find remote with open-policy-agent/gatekeeper.git
REMOTE_NAME=""
for remote in $(git remote); do
    URL=$(git remote get-url "$remote")
    if [[ "$URL" == *"open-policy-agent/gatekeeper.git" ]]; then
        REMOTE_NAME="$remote"
        break
    fi
done

if [[ -z "$REMOTE_NAME" ]]; then
    echo "Error: No remote found for open-policy-agent/gatekeeper.git"
    return 0
fi

while true; do
    read -rp "Detected remote '$REMOTE_NAME' for gatekeeper.git. Confirm? (y/n): " CONFIRM_REMOTE
    if [[ "$CONFIRM_REMOTE" == "y" || "$CONFIRM_REMOTE" == "Y" ]]; then
        break
    elif [[ "$CONFIRM_REMOTE" == "n" || "$CONFIRM_REMOTE" == "N" ]]; then
        while true; do
            read -rp "Enter the remote name you want to use: " CUSTOM_REMOTE
            if git remote | grep -q "^$CUSTOM_REMOTE$"; then
                REMOTE_NAME="$CUSTOM_REMOTE"
                echo "Using remote: $REMOTE_NAME"
                break
            else
                echo "Error: Remote '$CUSTOM_REMOTE' not found. Available remotes: $(git remote | tr '\n' ' ')"
            fi
        done
        break
    else
        echo "Please enter 'y' for yes or 'n' for no."
    fi
done

# 4. Fetch remote and checkout release branch
echo "Fetching from remote $REMOTE_NAME..."
if ! git fetch "$REMOTE_NAME"; then
    echo "❌ Error: Failed to fetch from remote $REMOTE_NAME"
    return 0
fi

echo "Checking out branch $RELEASE_BRANCH-tag..."
if ! git checkout -B "$RELEASE_BRANCH-tag" "$REMOTE_NAME/$RELEASE_BRANCH"; then
    echo "❌ Error: Failed to checkout branch $RELEASE_BRANCH-tag"
    return 0
fi

# 5. Check tag version against existing tags
echo "Checking tag version $NEW_TAG..."

# Check if tag already exists locally
if git tag --list | grep -q "^$NEW_TAG$"; then
    echo "⚠️  Warning: Tag $NEW_TAG already exists locally"
fi

if git ls-remote --tags "$REMOTE_NAME" | grep -q "refs/tags/$NEW_TAG$"; then
    echo "❌ Error: Tag $NEW_TAG already exists on remote $REMOTE_NAME"
    return 0
fi

# 6. Check git log for proper commit message
echo "Checking git commit history..."
if ! LATEST_COMMIT_MSG=$(git log -1 --pretty=format:"%s" 2>/dev/null); then
    echo "❌ Error: Failed to get latest commit message"
    return 0
fi

EXPECTED_COMMIT_MSG="chore: Prepare $NEW_TAG release"

echo "📋 Latest commit: $LATEST_COMMIT_MSG"
echo "🎯 Expected commit: $EXPECTED_COMMIT_MSG"

if [[ "$LATEST_COMMIT_MSG" == *"$EXPECTED_COMMIT_MSG"* ]]; then
    echo "✅ Commit message contains expected format"
else
    echo "❌ Error: Latest commit message doesn't contain expected format"
    echo "   Expected substring: 'chore: Prepare $NEW_TAG release'"
    echo "   Found: '$LATEST_COMMIT_MSG'"
    return 0
fi

# 7. Validate image tag in deploy/gatekeeper.yaml
echo "Validating image tag in deploy/gatekeeper.yaml..."

if [[ ! -f "deploy/gatekeeper.yaml" ]]; then
    echo "❌ Error: deploy/gatekeeper.yaml not found"
    return 0
fi

# Look for image lines with gatekeeper and extract the tag
IMAGE_LINES=$(grep -n "image:.*gatekeeper:" deploy/gatekeeper.yaml || true)

if [[ -z "$IMAGE_LINES" ]]; then
    echo "❌ Error: No gatekeeper image found in deploy/gatekeeper.yaml"
    return 0
fi

echo "📋 Found gatekeeper image references:"
echo "$IMAGE_LINES"

# Extract the tag from the image line (assuming format like "image: openpolicyagent/gatekeeper:v1.15.0")
FOUND_TAG=""
while IFS= read -r line; do
    if [[ "$line" =~ image:.*gatekeeper:([^[:space:]]+) ]]; then
        FOUND_TAG="${BASH_REMATCH[1]}"
        break
    fi
done <<< "$IMAGE_LINES"

if [[ -z "$FOUND_TAG" ]]; then
    echo "❌ Error: Could not extract tag from gatekeeper image"
    return 0
fi

echo "🏷️  Found image tag: $FOUND_TAG"
echo "🎯 Expected tag: $NEW_TAG"

if [[ "$FOUND_TAG" == "$NEW_TAG" ]]; then
    echo "✅ Image tag matches expected tag"
else
    echo "❌ Error: Image tag doesn't match expected tag"
    echo "   Expected: $NEW_TAG"
    echo "   Found: $FOUND_TAG"
    return 0
fi

echo "✅ All validations passed! Tag $NEW_TAG is properly prepared for release."

# 8. Create the tag
echo "Creating git tag $NEW_TAG..."
if ! git tag -a "$NEW_TAG" -m "$NEW_TAG"; then
    echo "❌ Error: Failed to create git tag $NEW_TAG"
    return 0
fi

echo "Checking git tag history..."
if ! git checkout "$NEW_TAG"; then
    echo "❌ Error: Failed to checkout tag $NEW_TAG"
    return 0
fi

if ! LATEST_COMMIT_MSG=$(git log -1 --pretty=format:"%s" 2>/dev/null); then
    echo "❌ Error: Failed to get latest commit message after checkout"
    return 0
fi

EXPECTED_COMMIT_MSG="chore: Prepare $NEW_TAG release"

echo "📋 Latest commit: $LATEST_COMMIT_MSG"
echo "🎯 Expected commit: $EXPECTED_COMMIT_MSG"

if [[ "$LATEST_COMMIT_MSG" == *"$EXPECTED_COMMIT_MSG"* ]]; then
    echo "✅ Commit message contains expected format"
else
    echo "❌ Error: Latest commit message doesn't contain expected format"
    echo "   Expected substring: 'chore: prepare $NEW_TAG release'"
    echo "   Found: '$LATEST_COMMIT_MSG'"
    return 0
fi

## Uncomment below section to push the tag to remote as well
# if ! git push "$REMOTE_NAME" "$NEW_TAG"; then
#     echo "❌ Error: Failed to push tag $NEW_TAG to remote $REMOTE_NAME"
#     return 0
# fi

echo "✅ Successfully created and pushed tag $NEW_TAG"