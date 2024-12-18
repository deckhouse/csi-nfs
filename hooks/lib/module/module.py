#
# Copyright 2023 Flant JSC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

from lib.module import values as module_values
import re
import os 

def get_values_first_defined(values: dict, *keys):
    return _get_first_defined(values, keys)

def _get_first_defined(values: dict, keys: tuple):
    for i in range(len(keys)):
        if (val := module_values.get_value(path=keys[i], values=values)) is not None:
            return val
    return

def get_https_mode(module_name: str, values: dict) -> str:
    module_path = f"{module_name}.https.mode"
    global_path = "global.modules.https.mode"
    https_mode = get_values_first_defined(values, module_path, global_path)
    if https_mode is not None:
        return str(https_mode)
    raise Exception("https mode is not defined")

def get_module_name() -> str:
    return "csi-nfs"
