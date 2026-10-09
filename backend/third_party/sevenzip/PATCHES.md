# Transferly patches to bodgit/sevenzip

Based on v1.6.5, BSD-3-Clause, with its original LICENSE and module metadata. Production sources and internal packages are retained;
upstream examples, samples and tests are not shipped here. Adapter/archive regressions exercise the patched behavior.

The preview boundary needs limits that the upstream API does not expose:

- Each folder uses at most four codecs and eight inputs/outputs; bind and packed-stream indices are checked before use.
- Slice-bound metadata integers are capped at 100,000 before allocation, instead of permitting values up to uint32's maximum.
- Raw and decoded headers are limited to 16 MiB before materialization.
- LZMA/LZMA2 and PPMd dictionaries are capped at 64 MiB; Zstandard uses a 64 MiB window, 128 MiB memory budget and one decoder worker.

Oversized headers or decoder dictionaries are refused; the original remote file can still be downloaded. These are preview admission
limits, not a claim of a fixed total process heap. Reconcile these patches and rerun hostile-header/normal-archive tests on upgrades.
Remove the local copy when upstream supplies equivalent configurable limits.
