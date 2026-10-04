# Pinned ComicInfo schemas

Upstream: https://github.com/anansi-project/comicinfo

Revision: `99e1453a163c777b4b5320a68732f6f133ac7918`

- `v2.1-draft/ComicInfo.xsd`: upstream `drafts/v2.1/ComicInfo.xsd`, SHA-256
  `c7a925b75a5297ec66b19898f59597077326a6b812d5ce77b3035fc179d75ed9`.
- `v2.0/ComicInfo.xsd`: upstream `schema/v2.0/ComicInfo.xsd`, SHA-256
  `3d9109effff705014f5f6076d92b0ad8c8dad90d993f2592d744c10bad45311c`.
- `LICENSE`: upstream MIT license, SHA-256
  `44711aa3256e18f540317e7aedd35f6c34f5d7aaaaad244521de80db81d98143`.

The default exporter uses the pinned 2.1 draft profile. 2.0 is an offline
comparison fixture, not an alternative export mode. Tests verify both hashes,
the complete serialization sequence and actual `xmllint --nonet` XSD results.
Install `libxml2-utils` for these tests on Debian/Ubuntu. Production does not
invoke xmllint and does not download schemas.
