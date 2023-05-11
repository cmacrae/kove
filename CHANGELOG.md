# Changelog

All notable changes to this project will be documented in this file.

This project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased](https://github.com/cmacrae/kove/compare/v0.2.1...HEAD)

## [0.2.1](https://github.com/cmacrae/kove/releases/tag/v0.2.1) - 2023-05-11

**Changed**
- Fix [hpack DoS vulnerability](https://security.snyk.io/vuln/SNYK-GOLANG-GOLANGORGXNETHTTP2HPACK-3358253)
- Update to Go 1.20 (thanks [@avestuk](https://github.com/avestuk)!)
- Fix metric deletion logic (thanks [@rcjames](https://github.com/rcjames)!)
- Add tests (thanks [@rcjames](https://github.com/rcjames)!)


## [0.2.0](https://github.com/cmacrae/kove/releases/tag/v0.2.0) - 2021-08-26

**Added**
- New `Data` field added to policy parser to expose arbitrary information, resulting in a `data` metric label
- Update to Go 1.17

## [0.1.0](https://github.com/cmacrae/kove/releases/tag/v0.1.0) - 2021-02-22

- Initial release! :tada:
