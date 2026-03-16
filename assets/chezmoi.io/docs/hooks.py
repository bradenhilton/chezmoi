from __future__ import annotations

import json
import re
import subprocess
import textwrap
import tomllib
from io import StringIO
from pathlib import Path, PurePosixPath

from mkdocs import utils
from mkdocs.config.defaults import MkDocsConfig
from mkdocs.structure.files import Files
from mkdocs.structure.pages import Page
from ruamel.yaml import YAML

non_website_paths = [
    'docs.go',
    'hooks.py',
    'reference/commands/commands.go',
    'reference/commands/commands_test.go',
]

templates = [
    'index.md',
    'install.md',
    'links/articles.md',
    'links/podcasts.md',
    'links/videos.md',
    'reference/configuration-file/variables.md',
    'reference/release-history.md',
]


def on_pre_build(config: MkDocsConfig, **kwargs) -> None:
    docs_dir = PurePosixPath(config.docs_dir)
    for src_path in templates:
        output_path = docs_dir.joinpath(src_path)
        template_path = output_path.parent / (output_path.name + '.tmpl')
        data_path = output_path.parent / (output_path.name + '.yaml')
        args = ['go', 'tool', 'execute-template']
        if Path(data_path).exists():
            args.extend(['-data', data_path])
        args.extend(['-output', output_path, template_path])
        subprocess.run(args, check=False)


def on_files(files: Files, **kwargs) -> Files:
    # remove non-website files
    for src_path in non_website_paths:
        files.remove(files.get_file_from_path(src_path))

    # remove templates and data
    for src_path in templates:
        files.remove(files.get_file_from_path(src_path + '.tmpl'))
        data_path = src_path + '.yaml'
        if data_path in files:
            files.remove(files.get_file_from_path(data_path))

    return files


def on_post_build(config: MkDocsConfig, **kwargs) -> None:
    config_dir = Path(config.config_file_path).parent
    site_dir = config.site_dir

    # copy GitHub pages config
    utils.copy_file(Path(config_dir, 'CNAME'), Path(site_dir, 'CNAME'))

    # copy installation scripts
    utils.copy_file(Path(config_dir, '../scripts/install.sh'), Path(site_dir, 'get'))
    utils.copy_file(
        Path(config_dir, '../scripts/install-local-bin.sh'),
        Path(site_dir, 'getlb'),
    )
    utils.copy_file(
        Path(config_dir, '../scripts/install.ps1'),
        Path(site_dir, 'get.ps1'),
    )

    # copy cosign.pub
    utils.copy_file(
        Path(config_dir, '../cosign/cosign.pub'),
        Path(site_dir, 'cosign.pub'),
    )


def on_page_markdown(markdown: str, page: Page, **kwargs) -> str:
    # Matches TOML fences surrounded by a HTML comment-style directive,
    # captures the initial indentation, optional filename title and TOML text.
    # We allow capturing *.toml.tmpl filename titles but do not support
    # converting examples containing any template syntax.
    example_pattern = re.compile(
        r"""
        ^([ \t]*)
        <!--\s*example-formats\s*-->
        \s*
        ```toml(?:[ \t]+title="([^"]+(?:\.toml(?:\.tmpl)?)?)")?
        \s*
        (.*?)
        \s*
        ```
        \s*
        <!--\s*/example-formats\s*-->
        """,
        re.MULTILINE | re.DOTALL | re.VERBOSE,
    )

    def rename_with_format(filename: str, fmt: str) -> str:
        for suffix in ('.toml.tmpl', '.toml'):
            if filename.endswith(suffix):
                return filename.removesuffix(suffix) + suffix.replace(
                    '.toml', f'.{fmt}'
                )
        return filename

    def build_code_fence(text: str, fmt: str, filename: str | None = None) -> str:
        title = rename_with_format(filename, fmt) if filename else None
        return '\n'.join(
            (f"""```{fmt}{f' title="{title}"' if title else ''}""", text.strip(), '```')
        )

    def build_example_tabs(toml_text: str, filename: str | None = None) -> str:
        data = tomllib.loads(toml_text)

        yaml_obj = YAML()
        yaml_obj.line_break = '\n'
        yaml_obj.width = 1024
        with StringIO() as yaml_stream:
            yaml_obj.dump(data, yaml_stream)
            yaml_text = yaml_stream.getvalue()

        json_text = json.dumps(data, indent=4)

        tabs = []
        for fmt, text in (
            ('toml', toml_text),
            ('yaml', yaml_text),
            ('json', json_text),
        ):
            # Indent each fence by 4 spaces in each tab, e.g.:
            # === "TOML"
            #
            #     ```toml
            fence = textwrap.indent(build_code_fence(text, fmt, filename), ' ' * 4)
            tabs.append(f'=== "{fmt.upper()}"\n\n{fence}')

        return '\n\n'.join(tabs)

    def replace(match: re.Match) -> str | None:
        indent = match.group(1)
        filename = match.group(2)
        toml_text = textwrap.dedent(indent + match.group(3)).strip()

        examples = build_example_tabs(toml_text, filename)

        return textwrap.indent(examples, indent)

    new_md = example_pattern.sub(replace, markdown)
    if new_md != markdown:
        new_file = Path('examples_preview') / page.file.src_uri
        new_file.parent.mkdir(parents=True, exist_ok=True)
        with new_file.open('w') as fp:
            fp.write(new_md)
    return new_md
