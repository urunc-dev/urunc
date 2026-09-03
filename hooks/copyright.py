# SPDX-License-Identifier: Apache-2.0

def on_config(config, **kwargs):
    author = config.get("site_author", "Author")
    config["copyright"] = f"Copyright &copy; {author}"
    return config
