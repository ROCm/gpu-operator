# Documentation Build Guide

This guide provides information for developers who want to contribute to the AMD GPU Operator documentation available at https://dcgpu.docs.amd.com/projects/gpu-operator. The docs use [rocm-docs-core](https://github.com/ROCm/rocm-docs-core) as their base and the below guide will show how you can build and serve the docs locally for testing.

## Building and Serving the Docs

1. Create a Python Virtual Environment (optional, but recommended)

    ```bash
    python3 -m venv .venv/docs
    source .venv/docs/bin/activate (or source .venv/docs/Scripts/activate on Windows)
    ```

2. Install required packages for docs

    ```bash
    pip install -r docs/sphinx/requirements.txt
    ```

3. Build the docs

    ```bash
    python3 -m sphinx -b html -d _build/doctrees -D language=en ./docs/ docs/_build/html
    ```

4. Serve docs locally on port 8000

    ```bash
    python3 -m http.server -d ./docs/_build/html/
    ```

5. You can now view the docs site by going to http://localhost:8000

## Auto-building the docs
The below will allow you to watch the docs directory and rebuild the documenatation each time you make a change to the documentation files:

1. Install Sphinx Autobuild package

    ```bash
    pip install sphinx-autobuild
    ```

2. Run the autobuild (will also serve the docs on port 8000 automatically)

    ```bash
    sphinx-autobuild -b html -d _build/doctrees -D language=en ./docs docs/_build/html --ignore "docs/_build/*" --ignore "docs/sphinx/_toc.yml"
    ```

## Troubleshooting

1. **Navigation Menu not displaying new links**

    Note that if you've recently added a new link to the navigation menu previously unchanged pages may not correctly display the new link. To fix this delete the existing `_build/` directory and rebuild the docs so that the navigation menu will be rebuilt for all pages.
