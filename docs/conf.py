"""Configuration file for the Sphinx documentation builder."""
import os

html_baseurl = os.environ.get("READTHEDOCS_CANONICAL_URL", "instinct.docs.amd.com")
html_context = {}
if os.environ.get("READTHEDOCS", "") == "True":
    html_context["READTHEDOCS"] = True
external_projects_local_file = "projects.yaml"
external_projects_remote_repository = ""
external_projects = ["amd-gpu-operator"]
external_projects_current_project = "amd-gpu-operator"

project = "AMD GPU Operator"
version = "1.4.0"
release = version
html_title = f"{project} {version}"
author = "Advanced Micro Devices, Inc."
copyright = "Copyright (c) 2025 Advanced Micro Devices, Inc. All rights reserved."

# Required settings
html_theme = "rocm_docs_theme"
html_theme_options = {
    "flavor": "instinct",
    "link_main_doc": True,
    "repository_url": "https://github.com/rocm/gpu-operator",
    # Add any additional theme options here
    "announcement": (
        "You're viewing documentation from a development branch. "
        "Please switch to the release branch for the officially released documentation."
    ),
    "show_navbar_depth": 0,
}
extensions = ["rocm_docs","sphinx_substitution_extensions"]

# Table of contents
external_toc_path = "./sphinx/_toc.yml"

exclude_patterns = ['.venv']

rst_prolog = """
.. |version| replace:: v{version}
""".format(version=version)
