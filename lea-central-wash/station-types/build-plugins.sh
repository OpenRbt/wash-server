#!/bin/bash
set -e

echo "Starting plugin build process..."

# Ищем все plugin.go файлы в папках server-config
find ./station-types -name "plugin.go" -path "*/server-config/*" | while read plugin_file; do
    plugin_dir=$(dirname "$plugin_file")
    output_file="$plugin_dir/plugin.so"

    echo ""
    echo "Building plugin: $plugin_file -> $output_file"

    if go build -trimpath -buildmode=plugin -o "$output_file" "$plugin_dir/..."; then
        echo "Successfully built: $output_file"

        if [ -f "$output_file" ]; then
            echo "Plugin verified: $output_file"
        else
            echo "ERROR: Plugin file was not created: $output_file" >&2
            exit 1
        fi
    else
        echo "ERROR: Failed to build plugin: $plugin_file" >&2
        exit 1
    fi
done

echo ""
echo "All plugins built successfully!"
