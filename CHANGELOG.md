# Changelog

## 1.0.0 (2026-05-20)


### Features

* **claim:** bootstrap source repo on claim (Phase 2c write-side) ([#5](https://github.com/MeKo-Tech/ewws-platform-ui/issues/5)) ([5803fd6](https://github.com/MeKo-Tech/ewws-platform-ui/commit/5803fd681fb1a0df5e9869c31d6c527344d28f53))
* **compliance:** periodic scanner + dashboard compliance column ([#3](https://github.com/MeKo-Tech/ewws-platform-ui/issues/3)) ([49ed22b](https://github.com/MeKo-Tech/ewws-platform-ui/commit/49ed22b11ff6c09eabb1bef60231ac51f022ca9f))
* **dashboard:** traffic + drift + activity overview, cards + table views ([#6](https://github.com/MeKo-Tech/ewws-platform-ui/issues/6)) ([6768791](https://github.com/MeKo-Tech/ewws-platform-ui/commit/676879183bd110ec826e2a7cad665fbbdf255495))
* **detail:** per-stage 7d/30d traffic chart + drift via SHA prefix ([#7](https://github.com/MeKo-Tech/ewws-platform-ui/issues/7)) ([556293b](https://github.com/MeKo-Tech/ewws-platform-ui/commit/556293b7808d6e18cd32cab8b40b483954d00265))
* GITHUB_API_TOKEN for reading the private registry; argo-cd token optional ([#2](https://github.com/MeKo-Tech/ewws-platform-ui/issues/2)) ([9d1532c](https://github.com/MeKo-Tech/ewws-platform-ui/commit/9d1532c2410983ded5ae9db7f7284ec8b6f0eebd))
* initial scaffold for the vibe-tenant platform UI ([765e0e8](https://github.com/MeKo-Tech/ewws-platform-ui/commit/765e0e87874b5470ac5edb0ba765ed8c73e8d490))


### Bug Fixes

* **ci:** bump Go toolchain to 1.25.x to match go.mod ([#4](https://github.com/MeKo-Tech/ewws-platform-ui/issues/4)) ([2bbee9d](https://github.com/MeKo-Tech/ewws-platform-ui/commit/2bbee9dc54a37d01f2a2ca1ad71b29d5669d7d3e))
* **lint:** compact long http.Error to satisfy wsl_v5 ([7768edf](https://github.com/MeKo-Tech/ewws-platform-ui/commit/7768edf4ee9792281ee068e8bed0377c98d6a4e9))
* **lint:** inline string concat in auth.go to fit wsl_v5 branch-max-lines ([aeb4b0b](https://github.com/MeKo-Tech/ewws-platform-ui/commit/aeb4b0b89cd4c6528421d4a7a87995c94a0f06f5))


### Refactor

* extract denyOrgAccess helper to satisfy both wsl_v5 + golines ([f1223db](https://github.com/MeKo-Tech/ewws-platform-ui/commit/f1223db848a95cd8ecc5ed2dae402aaa96f655a8))
