# Module Developer Guide
## Open Source Modular Application Platform v1.0

**Document:** 17-module-developer-guide.md
**Status:** Baseline (Rev 1.1)
**Previous:** 16-makefile-specification.md
**Next:** 18-testing-strategy.md
**Builds on:** 06-authorization-engine.md, 07-module-system.md, 10-backend-architecture.md §4

---

# 1. Purpose

07-module-system.md is the contract: what a module *is*, what it must declare, how core treats it. This document is the walkthrough: building one, start to finish, so the first module anyone writes doesn't require re-reading six other documents to find the one sentence that explains why a route isn't showing up.

The running example is **Announcements** — organization-wide posts a Group Admin can publish, deliberately chosen to be simpler than the Finance examples used elsewhere, so the pattern is visibly general rather than tied to one domain. It is also the platform's **reference module**: the repository's `modules/announcements/` is built by following this guide, and the guide is verified by doing exactly that from a clean checkout.

---

# 2. Before You Start

```text
make setup                       # 15 §4 — datastores up, migrations applied
make bootstrap-admin EMAIL=you@example.org
make dev                          # api + worker + frontend running
```

You should be able to log in as Super Admin on the running platform before writing any module code. If you can't, fix that first — a module can't be debugged on top of a broken base.

---

# 3. Scaffold It

```text
make module-create NAME=announcements
```

generates (16 §2.9) and runs `make modules-sync` so the module is wired into the build:

```text
modules/announcements/
├── module.yaml
├── backend/
│   ├── module.go
│   ├── domain/
│   ├── application/
│   ├── repository/
│   └── transport/
├── frontend/
│   ├── index.ts
│   └── routes/
├── migrations/
│   ├── 001_announcements_posts.up.sql
│   └── 001_announcements_posts.down.sql
├── permissions/
│   └── permissions.yaml
├── events/
├── tests/
└── README.md
```

The module code is a **lowercase identifier** (letters, digits, underscore — it becomes a table prefix and a migration-table name, 04 §7). Nothing here is functional yet — it's the shape every module has, so the next steps are "fill in," not "figure out where things go."

---

# 4. The Manifest

`modules/announcements/module.yaml`:

```yaml
id: announcements
name: Announcements
version: 0.1.0
description: Organization-wide announcements
author: Your Name
license: MIT

core:
  min_version: "1.0.0"

permissions:
  - code: announcement.read
    resource: announcement
    action: read
  - code: announcement.create
    resource: announcement
    action: create
  - code: announcement.publish
    resource: announcement
    action: publish
  - code: announcement.delete
    resource: announcement
    action: delete

migrations: ./migrations

backend:
  entrypoint: announcements.NewModule

frontend:
  entrypoint: ./frontend/index.ts
  routes:
    - path: /modules/announcements
      component: ./frontend/routes/posts/List.svelte
      requires_permission: announcement.read
  menu:
    - label: Announcements
      icon: megaphone
      route: /modules/announcements
      requires_permission: announcement.read

events:
  publishes:
    - announcements.published

notifications:
  - event: announcements.published
    type: announcements.published          # i18n key: notifications.announcements.published
    recipients: payload.recipient_user_ids
    priority: normal
    params: [title]
```

Everything downstream — the permission catalog, the menu, the notification that fires on publish — is driven from this file. If a step later doesn't seem to be working, check here first; a surprising number of "bugs" are a manifest field that wasn't updated after the code changed. Remember to run `make modules-sync` after editing it.

Rules the platform enforces on this file: permission codes must be in this module's own namespace (`announcement.*`, never `user.*`, `role.*`, …); the manifest is validated against the JSON Schema at startup (07 §4).

---

# 5. The Migration

`modules/announcements/migrations/001_announcements_posts.up.sql`:

```sql
CREATE TABLE announcements_posts (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    group_id        uuid,
    title           varchar(255) NOT NULL,
    body            text NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'published', 'archived')),
    created_by      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    published_at    timestamptz,
    row_version     bigint NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    -- the group (if any) must belong to the same organization
    CONSTRAINT fk_announcements_posts_group
        FOREIGN KEY (group_id, organization_id) REFERENCES groups (id, organization_id) ON DELETE CASCADE
);

CREATE INDEX idx_announcements_org_status ON announcements_posts (organization_id, status, created_at DESC);
SELECT attach_updated_at('announcements_posts');
```

`001_announcements_posts.down.sql`:

```sql
DROP TABLE announcements_posts;
```

Points that matter:

- `announcements_posts`, not `announcements` — the module-code prefix convention (07 §6) applies to the table name; if this module later needs a second table (`announcements_comments`, say), the prefix is what keeps every one of the module's tables grouped and unambiguous in a `\dt` listing.
- Migrations are **numbered from 001 within this module** and tracked in the module's own version table (`schema_migrations_announcements`, 07 §5.3) — you never coordinate numbers with core or other modules.
- Migrations run with the privileged migration role when the module is installed; the runtime `app_user` role automatically receives DML on the new table (04 §13.2). A module that wants an append-only table adds its own `REVOKE UPDATE, DELETE … FROM app_user` here.
- **Every table with `updated_at` must call `attach_updated_at`** (the line above). A CI test fails if you forget.
- `group_id` references core's `groups` through a composite foreign key — reading and referencing core's own tables is fine; it's writing to, or reaching past, *another module's* tables that's the isolation rule (07 §6).

---

# 6. The Domain

`modules/announcements/backend/domain/post.go`:

```go
package domain

type Post struct {
    ID             uuid.UUID
    OrganizationID uuid.UUID
    GroupID        *uuid.UUID
    Title          string
    Body           string
    Status         string
    CreatedBy      uuid.UUID
    PublishedAt    *time.Time
    Version        int64
}

var ErrAlreadyPublished = errors.New("post is already published")

func (p *Post) Publish(now time.Time) error {
    if p.Status == "published" {
        return ErrAlreadyPublished
    }
    p.Status = "published"
    p.PublishedAt = &now
    return nil
}
```

No `context.Context`, no database, no HTTP — this is the layer that's cheap to unit test (10 §14) and the layer where "can't publish something twice" lives as a rule, not as a scattered `if` check repeated at every call site.

---

# 7. The Repository

```go
package repository

type PostRepository interface {
    FindByID(ctx context.Context, organizationID, id uuid.UUID) (*domain.Post, error)
    Save(ctx context.Context, post *domain.Post) error
    List(ctx context.Context, organizationID uuid.UUID, filter ListFilter) ([]*domain.Post, error)
}
```

Every method takes `organizationID` as an explicit argument (12 §6) — there is no method on this interface that can return or modify a row without it, by construction, not by remembering to add a `WHERE` clause correctly every time. The implementation uses `modulesdk.DB` (10 §4), which is transaction-aware: inside `Tx.WithinTx` the same calls run in the surrounding transaction.

```go
func (r *postRepo) FindByID(ctx context.Context, orgID, id uuid.UUID) (*domain.Post, error) {
    row := r.db.QueryRow(ctx,
        `SELECT id, organization_id, group_id, title, body, status, created_by, published_at, row_version
           FROM announcements_posts
          WHERE organization_id = $1 AND id = $2`, orgID, id)
    // scan … ; map "no rows" to a not-found error
}
```

---

# 8. The Application Service

This is where `module-sdk`'s `CoreDeps` (10 §4) actually gets used. State change, audit record and event are **one transaction** (10 §6.1):

```go
package application

type PublishPost struct {
    Repo repository.PostRepository
    Deps modulesdk.CoreDeps
}

func (uc *PublishPost) Execute(ctx context.Context, identity modulesdk.Identity, scope modulesdk.ScopeContext, postID uuid.UUID) error {
    decision, err := uc.Deps.Authz.Can(ctx, modulesdk.Check{
        Identity: identity, Scope: scope, Action: "announcement.publish",
        Resource: &modulesdk.ResourceRef{Type: "announcement", ID: postID, OrganizationID: scope.OrganizationID},
    })
    if err != nil {
        return err
    }
    if !decision.Allowed {
        return modulesdk.ErrDenied   // the denial is audited by the middleware/recorder in its own transaction
    }

    return uc.Deps.Tx.WithinTx(ctx, func(ctx context.Context) error {
        post, err := uc.Repo.FindByID(ctx, *scope.OrganizationID, postID)
        if err != nil {
            return err
        }
        if err := post.Publish(time.Now()); err != nil {
            return err
        }
        if err := uc.Repo.Save(ctx, post); err != nil {   // UPDATE … WHERE row_version = $n → version_conflict
            return err
        }
        uc.Deps.Audit.Record(ctx, modulesdk.ActivityEntry{
            Action: "announcement.published", ResourceType: "announcement", ResourceID: post.ID,
            Result: "success",
        })
        uc.Deps.Events.Publish(ctx, modulesdk.Event{   // written to the outbox in THIS transaction
            Name: "announcements.published",
            Payload: map[string]any{
                "post_id": post.ID, "organization_id": post.OrganizationID,
                "title": post.Title, "recipient_user_ids": recipients, // resolved by the module (e.g. all active org members)
            },
        })
        return nil
    })
}
```

That `Events.Publish` call is the entire integration with the notification system (07 §8, 09 §6) — this module never calls a notification API directly. The `notifications:` section of the manifest (§4) is the Notification Template: when the worker's outbox relay delivers `announcements.published`, core turns it into an in-app notification for each recipient. The module publishes a fact; it doesn't need to know who's listening or how they're told. If the transaction rolls back, **no** event is ever delivered; if the worker is down, the event waits in the outbox and nothing is lost.

---

# 9. The Handler & Routes

```go
func (m *Module) RegisterRoutes(r modulesdk.Router, deps modulesdk.CoreDeps) {
    r.Post("/api/v1/announcements/{id}/publish", m.handlePublish(deps),
        modulesdk.Require("announcement.publish"), modulesdk.Idempotent())
    // … list/create/get/delete follow the same shape, each with its own Require(...)
}

func (m *Module) handlePublish(deps modulesdk.CoreDeps) http.HandlerFunc {
    uc := &application.PublishPost{Repo: m.repo, Deps: deps}
    return func(w http.ResponseWriter, r *http.Request) {
        identity, _ := modulesdk.IdentityFromContext(r.Context())
        scope, _ := modulesdk.ScopeFromContext(r.Context())

        id, err := uuid.Parse(modulesdk.URLParam(r, "id"))
        if err != nil {
            modulesdk.WriteError(w, modulesdk.ErrValidation("id", "invalid_uuid"))   // 400 validation_failed — never a panic
            return
        }
        if err := uc.Execute(r.Context(), identity, scope, id); err != nil {
            modulesdk.WriteError(w, err)   // maps known errors to the standard envelope; unknown → internal_error
            return
        }
        w.WriteHeader(http.StatusNoContent)
    }
}
```

Note what's present and absent:

- **`Require("announcement.publish")`** is the declared permission for the route; the core middleware checks it before your handler runs (06 §9). A route registered without `Require(...)` or an explicit `Public()` is **rejected at startup** (07 §5.1). `Idempotent()` makes the platform require an `Idempotency-Key` header and replay the stored response on a retry (08 §6).
- No session lookup, no cookie parsing, no manual authorization query, no router import. Authentication and the base authorization middleware already ran. The `Authz.Can` call inside `PublishPost.Execute` is the **resource-specific** check (this action, on this exact post), on top of the generic "is this endpoint reachable at all" check.
- The module wrapper (07 §5.4) already guarantees the module is enabled for the caller's organization — you do not check that yourself.
- Write `swag` annotations on every handler (08 §9) so your endpoints appear in the generated OpenAPI document and TypeScript SDK; `make generate` then `git diff` must be clean.

---

# 10. Your First Tests

```go
func TestPost_Publish_AlreadyPublished(t *testing.T) {
    post := &domain.Post{Status: "published"}
    err := post.Publish(time.Now())
    require.ErrorIs(t, err, domain.ErrAlreadyPublished)
}

func TestPublishPost_Denied(t *testing.T) {
    deps := modulesdk.CoreDeps{Authz: fakeAuthorizer{allow: false}, Tx: fakeTx{}}
    uc := &application.PublishPost{Deps: deps}
    err := uc.Execute(context.Background(), fakeIdentity, fakeScope, uuid.New())
    require.ErrorIs(t, err, modulesdk.ErrDenied)
}

// Repository test against a real, throwaway PostgreSQL (10 §14) — the negative tenant test is mandatory:
func TestPostRepo_CrossTenantRead(t *testing.T) {
    db := testkit.NewPostgres(t)               // runs core + this module's migrations
    orgA, orgB := fixtures.NewOrganization(t, db), fixtures.NewOrganization(t, db)
    post := fixtures.NewPost(t, db, orgA)
    _, err := repo.FindByID(ctx, orgB.ID, post.ID)
    require.ErrorIs(t, err, repository.ErrNotFound)   // Organization B can never see Organization A's post
}
```

`make test-backend` (16 §2.3) picks this up automatically — `go test ./...` from the repository root includes `modules/…`; there is no separate registration step. The `attach_updated_at` catalog test and the table-prefix isolation lint (18 §6) also run against your module.

---

# 11. The Frontend

Following the six-file pattern (07 §11), scaffolded for you:

```text
modules/announcements/frontend/routes/posts/
├── List.svelte           # table of posts, PermissionGate around "New" button
├── Detail.svelte
├── Form.svelte
├── Modal.svelte           # publish/delete confirmation
├── PermissionGate.svelte   # re-exported from packages/ui, not reimplemented per module
└── Breadcrumb.ts
```

```svelte
<!-- List.svelte, excerpt -->
<PermissionGate permission="announcement.create">
  <a href="/modules/announcements/new">{$t('announcements.actions.new')}</a>
</PermissionGate>
```

Strings are translation keys, not literals; ship a catalog (`frontend/i18n/en-US.json`, `id-ID.json`) and list it in the manifest so it is merged at registration (11 §10). The notification text for `announcements.published` is also an i18n key (`notifications.announcements.published`) rendered from the notification's `data` (`{ title }`).

The list itself renders whatever fields the API returned — if a future field permission ever restricts a column on this resource, this component needs no changes at all (06 §7, 11 §6). For a per-post "Publish" button use the `meta.allowed_actions` the detail response carries (08 §3.1) rather than the session-level `can()`.

---

# 12. Install & Enable Locally

```text
make modules-sync                                                   # if you changed module.yaml
POST /api/v1/modules/announcements/install                           (Super Admin)
POST /api/v1/organizations/{org_id}/modules/announcements/enable      (Super Admin or Org Admin)
```

or the equivalent buttons in the "Manage Modules" admin UI. On `install`, the migration runs (with the migration role) and the four permissions from the manifest are upserted into `permissions` (07 §7) — log in again (or refresh the session) afterward, since the effective-permission set is computed at login/refresh, not re-read on every click.

"Install" activates a module that is **already compiled into the binary** — restart `make dev` (the file watcher normally does this) after adding or changing a module.

---

# 13. Verify End-to-End

```text
[ ] Menu item appears only for a user who holds announcement.read
[ ] "New Announcement" is hidden for a user without announcement.create,
    and POSTing directly to the create endpoint as that user still 403s
    (frontend hiding it is not the check — 06 §15)
[ ] Publishing creates a notification other organization members can see (via the worker/outbox; check Mailpit if email is enabled)
[ ] A retried publish with the same Idempotency-Key does not publish twice
[ ] Disabling the module for the organization hides the menu and 404s the routes — for that organization only
[ ] make test-backend includes and passes this module's tests (including the cross-tenant test)
[ ] make lint passes the isolation lint (no table outside announcements_*, except allowlisted core reads)
[ ] make generate leaves no diff (annotations present, registries synced)
```

---

# 14. Common Pitfalls

```text
"My new permission doesn't show up in the role editor."
    → permissions are synced at install/enable time (07 §7).
      Re-run install, or re-enable, after editing the manifest — and make modules-sync first.

"My route 404s even though I'm logged in as Super Admin."
    → is the module actually enabled for the organization you're testing in?
      Super Admin bypasses nothing in the module lifecycle itself (07 §5.4).

"The server refuses to start: route has no Require()."
    → every module route declares Require(permission) or Public() (07 §5.1).

"I need data from another module."
    → don't query its tables. Subscribe to the event it already publishes
      (07 §8), or ask for a core-exposed interface if the data is core's, not another module's.

"My test can see another organization's data."
    → your repository method is missing an organization_id filter — this is
      exactly the negative test 12 §6 requires for every new tenant-scoped table.

"My notification never arrives."
    → events are delivered by the WORKER via the outbox: is `make run-worker` (or `make dev`) running?
      Check outbox_events for pending/dead rows and the worker log.

"The CI says updated_at trigger missing."
    → add SELECT attach_updated_at('<table>'); to the migration that creates the table (04 §4).

"My handler panics on a bad id."
    → parse with uuid.Parse and return a validation error; never uuid.MustParse on user input.
```

---

# 15. Sharing Your Module

The module marketplace is future work (20 §5), but nothing stops sharing a module informally today — a git repository someone else copies into their own `modules/` directory (then `make modules-sync`). Before doing that:

```text
[ ] module.yaml's version follows semver, and core.min_version is accurate
[ ] No organization-specific values are hardcoded anywhere (check settings.schema.json
    covers everything that should be configurable instead)
[ ] README.md explains what the module does and lists its permissions in plain language,
    not just as a code list — an installing Super Admin is deciding whether to trust it
[ ] Tests exist and pass via a plain `go test ./...` including cross-tenant negative tests
[ ] No table is read or written outside this module's own prefix (07 §6)
[ ] No permission code outside the module's own namespace (07 §7)
[ ] Every route declares Require(...) and carries swag annotations
[ ] The module ships its own i18n catalogs for its UI strings and notification types
```

That list is exactly what a future reviewed marketplace would check first — getting it right now costs nothing and means there's nothing to fix later when that day comes.
