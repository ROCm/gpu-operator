#!/usr/bin/python3

'''
 Copyright (c) Advanced Micro Devices, Inc. All rights reserved.

 Licensed under the Apache License, Version 2.0 (the \"License\");
 you may not use this file except in compliance with the License.
 You may obtain a copy of the License at

      http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an \"AS IS\" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
'''

import pytest
import logging
import lib.dra_util as dra_util

Logger = logging.getLogger("gpu-operator.conftest")


@pytest.fixture(scope="session")
def dra_api_version(environment):
    """Detect and cache DRA API version."""
    if hasattr(environment, "dra_api_version"):
        return environment.dra_api_version
    dra_available, error_msg, api_version = dra_util.check_dra_api_available()
    if not dra_available:
        pytest.fail(f"DRA API not available: {error_msg}")
    setattr(environment, "dra_api_version", api_version)
    return api_version


def pytest_html_report_title(report):
    # Add a custom title to the report
    report.title = f"AMD GPU Operator/DeviceConfig Validation Test Results"

@pytest.fixture(scope="session", autouse=True)
def setup_techsupport_args(request, gpu_operator_release_name, environment):
    if environment.tech_support_tool:
        environment.tech_support_tool["args"] = ["all"]

