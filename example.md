# Gemini CLI example for osc-mcp


## User
Check home:mslacken:test folly for errors and fix them. After this commit the fixed version

## Gemini
I'll check `home:mslacken:test`'s `folly` package for errors, fix them, and commit. First, I'll pull the build log to identify the errors.
```json
{
  "tool_code": "print(gemini.get_build_log(project_name='home:mslacken:test', package_name='folly'))"
}
```

## OSC-MCP
```json
{
  "tool_result": {
    "error": "MCP tool 'get_build_log' reported tool error for function call: {\"name\":\"get_build_log\",\"args\":{\"project_name\":\"home:mslacken:test\",\"package_name\":\"folly\"}} with response: [{\"functionResponse\":{\"name\":\"get_build_log\",\"response\":{\"error\":{\"content\":[{\"type\":\"text\",\"text\":\"package is unresolvable, but could not determine missing dependencies. Details: nothing provides glog-4-devel\"}],\"isError\":true}}}}]"
  }
}
```

## Gemini
`glog-4-devel` is a missing dependency. I'll use `search_packages` to find it in `openSUSE:Tumbleweed`.
```json
{
  "tool_code": "print(gemini.search_packages(pattern='glog-devel', path='openSUSE:Tumbleweed'))"
}
```

## OSC_MCP
```json
{
  "tool_result": {
    "error": "params must have required property 'path_repository'"
  }
}
```

## Gemini
Oops, `search_packages` failed. I forgot `path_repository`. I'll find the right openSUSE Tumbleweed repository by listing them, then retry.
```json
{
  "tool_code": "print(gemini.get_project_meta(project_name='openSUSE:Tumbleweed'))"
}
```

## OSC-MCP
```json
{
  "tool_result": {
    "output": "{\"name\":\"openSUSE:Tumbleweed\",\"exists\":false,\"title\":\"Tumbleweed\",\"description\":\"Tumbleweed is the openSUSE Rolling Release\n\nThis OBS Project represents the content of the currently published\nsnapshot. The newer repository for next publish can be found in openSUSE:Factory standard repository.\n\",\"maintainers\":[\"dimstar_suse\"],\"repositories\":[{\"name\":\"standard\",\"path_project\":\"openSUSE:Tumbleweed\",\"path_repository\":\"dod\",\"arches\":[\"i586\",\"x86_64\",\"aarch64\",\"armv7l\",\"armv6l\",\"riscv64\",\"s390x\",\"ppc\",\"ppc64le\"]},{\"name\":\"slowroll-next\",\"arches\":[\"x86_64\"]},{\"name\":\"slowroll-202406\",\"arches\":[\"x86_64\"]},{\"name\":\"slowroll\",\"arches\":[\"x86_64\"]},{\"name\":\"dod_debug\",\"arches\":[\"i586\",\"x86_64\",\"aarch64\",\"armv7l\",\"armv6l\",\"riscv64\"]},{\"name\":\"dod\",\"arches\":[\"i586\",\"x86_64\",\"aarch64\",\"armv7l\",\"armv6l\",\"riscv64\",\"s390x\",\"ppc\",\"ppc64le\"]}]}"
  }
}
```

## Gemini
`openSUSE:Tumbleweed` repositories are visible. I'll search the `standard` repository for the `glog-devel` package.
```json
{
  "tool_code": "print(gemini.search_packages(path='openSUSE:Tumbleweed', path_repository='standard', pattern='glog-devel'))"
}
```

## OSC-MCP
```json
{
  "tool_result": {
    "output": "{\"packages\":[{\"Name\":\"glog-devel\",\"Arch\":\"x86_64\",\"Version\":\"0.7.1-1.1\"}]}"
  }
}
```

## Gemini
Found `glog-devel`, not `glog-4-devel`. Now, I'll check out `folly` to fix the dependency.
```json
{
  "tool_code": "print(gemini.checkout_bundle(project_name='home:mslacken:test', package_name='folly'))"
}
```

## OSC-MCP
```json
{
  "tool_result": {
    "output": "{\"path\":\"/tmp/mcp/osc-mcp/home:mslacken:test/folly\",\"package_name\":\"folly\",\"project_name\":\"home:mslacken:test\"}"
  }
}
```

## Gemini
Okay, package checked out. I'll find the spec file using `list_source_files` and then edit the dependency from `glog-4-devel` to `glog-devel`.
```json
{
  "tool_code": "print(gemini.list_source_files(package_name='folly', project_name='home:mslacken:test'))"
}
```

## OSC-MCP
```json
{
  "tool_result": {
    "output": "{\"project_name\":\"home:mslacken:test\",\"package_name\":\"folly\",\"files\":[{\"name\":\"_link\",\"size\":\"124\",\"md5\":\"e4ab98d951de858030fec28e9a5b7ce4\",\"mtime\":\"1757690244\",\"content\":\"<link project=\\\"home:mslacken:ml\\" baserev=\\\"7c0d6c7f8e887dab2a0fd1172b0d6c79\\\">\\n  <patches>\\n    <branch/>\\n  </patches>\\n</link>\\n\"},{\"name\":\"_service\",\"size\":\"746\",\"md5\":\"f50c089e836527659742d9d99ece0e56\",\"mtime\":\"1722605179\",\"content\":\"<services>\\n  <service mode=\\\"disabled\" name=\\\"obs_scm\">\\n    <param name=\"url\">https://github.com/facebook/folly</param>\\n    <param name=\"scm\">git</param>\\n    <param name=\"changesgenerate\">enable</param>\\n    <param name=\"filename\">folly</param>\\n    <param name=\"versionformat\">@PARENT_TAG@</param>\\n    <param name=\"versionrewrite-pattern\">v(.*)</param>\\n    <param name=\"revision\">v2024.07.22.00</param>\\n    <param name=\"package-meta\">yes</param>\\n  </service>\\n  <service name=\"set_version\" mode=\"manual\" />\\n  <service mode=\"buildtime\" name=\"tar\" />\\n  <service mode=\"buildtime\" name=\"recompress\">\\n    <param name=\"file\">*.tar</param>\\n    <param name=\"compression\">xz</param>\\n  </service>\\n  <service mode=\"manual\" name=\"format_spec_file\"/>\\n</services>\\n\"},{\"name\":\"folly-2024.07.22.00.obscpio\",\"size\":\"37081613\",\"md5\":\"5afe769f7d228af59abf11261ba020aa\",\"mtime\":\"1722605180\"},{\"name\":\"folly.changes\",\"size\":\"1124\",\"md5... [truncated]"
  }
}
```

## Gemini
Okay, `folly.spec`, I'm replacing `glog-4-devel` with `glog-devel`.
```json
{
  "tool_code": "print(gemini.replace(instruction='In the 'folly.spec' file, correct the glog dependency by updating the 'BuildRequires' from 'glog-4-devel' to 'glog-devel' to fix the build error.', old_string='BuildRequires:  glog-4-devel', file_path='/tmp/mcp/osc-mcp/home:mslacken:test/folly/folly.spec', new_string='BuildRequires:  glog-devel'))"
}
```

## GEMINI-TOOL
```json
{
  "tool_result": {
    "output": "Successfully modified file: /tmp/mcp/osc-mcp/home:mslacken:test/folly/folly.spec (1 replacements)."
  }
}
```

## Gemini
Dependency fixed. Committing.
```json
{
  "tool_code": "print(gemini.commit(package_name='folly', message='Fix glog dependency', directory='/tmp/mcp/osc-mcp/home:mslacken:test/folly', project_name='home:mslacken:test'))"
}
```

