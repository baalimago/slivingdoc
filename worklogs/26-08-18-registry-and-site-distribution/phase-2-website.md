# Phase 2 — Website distribution surface

**Status:** Not Started

**Worklog:** [README](README.md)

## Goal

Make social shares of `slivingdoc.dev` render a useful preview and point every
evaluation path at the current SeaweedFS example.

## Specification

The website source must:

- replace the live `examples/minio` link with `examples/seaweedfs`;
- set an absolute `og:image` that communicates the shared-workspace benefit;
- set `twitter:card` to `summary_large_image` and matching Twitter image/title/
  description metadata;
- keep the canonical URL, Open Graph URL, page title, and share text mutually
  consistent; and
- preserve the user’s independently produced terminal recording.

The site source/deployment path is not in the current workspace. Do not create
an unrelated replacement site or attempt a live-hosting mutation without it.

## Integration contract

| Trigger | Collaborators | Observable result | Required side effect | Prohibited side effect |
| --- | --- | --- | --- | --- |
| LinkedIn/X crawler requests the page | Static site and social-card asset | A title, description, and image card render. | Serve absolute image metadata. | Depend on client-side JS for card fields. |
| Visitor selects local evaluation | GitHub example link | The current SeaweedFS README opens. | Link to `examples/seaweedfs`. | Link to removed `examples/minio`. |

## Acceptance criteria

- [ ] The source has complete Open Graph and Twitter card fields, including an absolute image URL. — blocked on source location
- [ ] The site refers only to `examples/seaweedfs`. — blocked on source location
- [ ] A deployed-page inspection confirms the rendered metadata and link target. — blocked on source location

## Error coverage

| Failure | Expected outcome | Required check |
| --- | --- | --- |
| Social-card image URL is relative or unavailable | Crawler cannot render a rich card. | Inspect deployed meta tags and image URL. |
| Legacy MinIO link remains | Evaluation path reaches a 404. | Follow the deployed link. |
| Source/deployment path is unavailable | No live mutation is attempted. | Record the blocker and request the path. |

## Implementation notes

- Pending source location or deployment access.

## Review findings

No reviews recorded.
