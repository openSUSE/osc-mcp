# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1] - 19.9.2025

### Changed
- create_bundle now includes the created files
- more explicit language in defaults.yaml

## [0.1.0] - 18.9.2025

### Added

- Initial release of the MCP server for Open Build Service.
- search_bundle: Search bundles on remote OBS instance or local bundles.
- list_source_files: List source files of given bundle in local or remote location.
- branch_bundle: Branch a bundle and check it out as a local bundle.
- build_bundle: Build a source bundle.
- get_project_meta: Get the metadata of a project.
- set_project_meta: Set the metadata for the project.
- create_bundle: Create a new local bundle.
- checkout_bundle: Checkout a package from the online repository.
- get_build_log: Get the remote or local build log of a package.
- search_packages: Search the available packages for a remote repository.
- commit: Commits changed files.
