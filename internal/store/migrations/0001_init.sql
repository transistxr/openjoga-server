-- +goose Up


-- One row. A single self-hosted instance is one community.
CREATE TABLE community (
  id         INTEGER PRIMARY KEY CHECK (id = 1),
  name       TEXT NOT NULL,
  currency   TEXT NOT NULL DEFAULT 'INR',
  settings   TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);


-- A user starts device-only: a row created on first RSVP, reachable only by the
-- cookie on that browser. It becomes durable when a contact is verified.
CREATE TABLE users (
  id           INTEGER PRIMARY KEY,
  display_name TEXT NOT NULL,

  pronouns TEXT,

  email              TEXT UNIQUE,
  email_verified_via TEXT CHECK (email_verified_via IN ('self','admin')),
  email_verified_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  email_verified_at  TEXT,

  phone              TEXT UNIQUE,
  phone_verified_via TEXT CHECK (phone_verified_via IN ('admin')),
  phone_verified_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  phone_verified_at  TEXT,

  is_device_only INTEGER NOT NULL DEFAULT 1 CHECK (is_device_only IN (0,1)),
  community_role TEXT NOT NULL DEFAULT 'member'
                   CHECK (community_role IN ('owner','admin','member')),
  created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);


CREATE TABLE groups (
  id INTEGER PRIMARY KEY,

  slug TEXT NOT NULL UNIQUE,

  name             TEXT NOT NULL,
  sport            TEXT,
  description      TEXT,
  visibility       TEXT NOT NULL DEFAULT 'unlisted'
                     CHECK (visibility IN ('public','private','unlisted')),
  default_location TEXT,

  default_cost_type           TEXT NOT NULL DEFAULT 'free'
                                CHECK (default_cost_type IN ('free','fixed','split')),
  default_cost_per_head_pence INTEGER,
  default_cost_total_pence    INTEGER,

  max_guests_per_rsvp   INTEGER NOT NULL DEFAULT 3 CHECK (max_guests_per_rsvp >= 0),
  max_slots_per_sponsor INTEGER NOT NULL DEFAULT 4 CHECK (max_slots_per_sponsor >= 0),

  payment_kind  TEXT CHECK (payment_kind IN ('upi','bank','paypal','revolut','text')),
  payment_value TEXT,
  payment_label TEXT,

  track_kitty INTEGER NOT NULL DEFAULT 0 CHECK (track_kitty IN (0,1)),
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);


-- Token-based join links. A QR code is just this token rendered as a picture;
-- the backend returns the URL and the frontend draws it.
CREATE TABLE invites (
  id         INTEGER PRIMARY KEY,
  token_hash TEXT NOT NULL UNIQUE,

  kind  TEXT NOT NULL DEFAULT 'durable' CHECK (kind IN ('rotating','durable')),
  scope TEXT NOT NULL CHECK (scope IN ('community','group')),

  group_id          INTEGER REFERENCES groups(id) ON DELETE CASCADE,
  role              TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('organiser','member')),
  max_uses          INTEGER,
  uses              INTEGER NOT NULL DEFAULT 0,
  requires_approval INTEGER NOT NULL DEFAULT 1 CHECK (requires_approval IN (0,1)),

  attests_contact INTEGER NOT NULL DEFAULT 0 CHECK (attests_contact IN (0,1)),

  label      TEXT,
  expires_at TEXT,
  revoked_at TEXT,
  created_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),

  CHECK (scope <> 'group' OR group_id IS NOT NULL)
);
CREATE INDEX idx_invites_group ON invites(group_id);


CREATE TABLE group_members (
  id       INTEGER PRIMARY KEY,
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id  INTEGER NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
  role     TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('organiser','member')),

  -- 'pending' is where an unvetted joiner waits for an admin. Only 'active'
  -- members count as members anywhere else in the system.
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('pending','active')),

  invite_id INTEGER REFERENCES invites(id) ON DELETE SET NULL,
  joined_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  UNIQUE (group_id, user_id)
);
CREATE INDEX idx_group_members_user   ON group_members(user_id);
CREATE INDEX idx_group_members_status ON group_members(group_id, status);


CREATE TABLE events (
  id       INTEGER PRIMARY KEY,
  group_id INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  slug     TEXT NOT NULL UNIQUE,
  title    TEXT,
  status   TEXT NOT NULL DEFAULT 'scheduled'
             CHECK (status IN ('draft','scheduled','confirmed','cancelled','completed')),

  starts_at      TEXT NOT NULL,
  ends_at        TEXT NOT NULL,
  timezone       TEXT NOT NULL DEFAULT 'Europe/London',
  location       TEXT,
  min_players    INTEGER,
  max_players    INTEGER,
  rsvp_cutoff_at TEXT,

  -- How much identity it takes to claim a slot.
  --   open          anyone with the link, name only
  --   contactable   needs a verified email, an attested phone, a push
  --                 subscription, or to be somebody's guest
  --   members_only  active member of the group
  rsvp_policy TEXT NOT NULL DEFAULT 'open'
                CHECK (rsvp_policy IN ('open','contactable','members_only')),

  --   free   no cost
  --   fixed  cost_per_head_pence, known from the moment the event is created
  --   split  cost_total_pence divided at cutoff among whoever is going.
  --          Deferred fixed, not dynamic: no per-head number is ever shown live
  --          (players see a range from min/max_players), and once resolved it
  --          behaves exactly like 'fixed'.
  cost_type           TEXT NOT NULL DEFAULT 'free'
                        CHECK (cost_type IN ('free','fixed','split')),
  cost_per_head_pence INTEGER,
  cost_total_pence    INTEGER,

  -- Let players see payment details before the game rather than at it. Requires a
  -- gated guest list so the organiser knows exactly who their VPA reached.
  show_payment_pre_game INTEGER NOT NULL DEFAULT 0
                          CHECK (show_payment_pre_game IN (0,1)),

  cancelled_reason TEXT,
  created_by       INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  updated_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),

  CHECK (cost_type <> 'fixed' OR cost_per_head_pence IS NOT NULL),
  CHECK (cost_type <> 'split' OR (cost_total_pence IS NOT NULL AND min_players IS NOT NULL)),
  CHECK (show_payment_pre_game = 0 OR rsvp_policy <> 'open')
);
CREATE INDEX idx_events_group_start ON events(group_id, starts_at);
CREATE INDEX idx_events_worker      ON events(status, rsvp_cutoff_at);


CREATE TABLE sponsorships (
  id              INTEGER PRIMARY KEY,
  event_id        INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  sponsor_user_id INTEGER NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
  slots           INTEGER NOT NULL CHECK (slots > 0),

  status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','active','revoked')),

  note        TEXT,
  approved_by INTEGER REFERENCES users(id) ON DELETE SET NULL,
  approved_at TEXT,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_sponsorships_event ON sponsorships(event_id, status);


CREATE TABLE rsvps (
  id       INTEGER PRIMARY KEY,
  event_id INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id  INTEGER NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
  status   TEXT NOT NULL CHECK (status IN ('going','maybe','out','waitlisted')),

  waitlisted_at TEXT,                               -- FIFO promotion order
  attended      INTEGER CHECK (attended IN (0,1)),  -- null until marked

  -- Set when this player took a slot somebody else is covering. The claim lives
  -- here, not on payments, because it is made when the slot is taken and no
  -- payment row exists until the cutoff.
  sponsorship_id INTEGER REFERENCES sponsorships(id) ON DELETE SET NULL,

  responded_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  UNIQUE (event_id, user_id)
);
CREATE INDEX idx_rsvps_event_status ON rsvps(event_id, status);
CREATE INDEX idx_rsvps_user         ON rsvps(user_id);
CREATE INDEX idx_rsvps_sponsorship  ON rsvps(sponsorship_id);


-- A +1: eats a slot, fee lands on the host's tab, name optional.
CREATE TABLE guests (
  id         INTEGER PRIMARY KEY,
  rsvp_id    INTEGER NOT NULL REFERENCES rsvps(id) ON DELETE CASCADE,
  name       TEXT,                                   -- null renders as "Host's +1"
  attended   INTEGER CHECK (attended IN (0,1)),
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_guests_rsvp ON guests(rsvp_id);


-- Tracking only;
CREATE TABLE payments (
  id               INTEGER PRIMARY KEY,
  event_id         INTEGER NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  user_id          INTEGER NOT NULL REFERENCES users(id)  ON DELETE CASCADE,
  amount_due_pence INTEGER NOT NULL,
  status           TEXT NOT NULL DEFAULT 'owed'
                     CHECK (status IN ('owed','paid','waived')),

  -- Set when a sponsorship covers this player
  sponsorship_id INTEGER REFERENCES sponsorships(id) ON DELETE SET NULL,

  marked_paid_at TEXT,
  marked_by      INTEGER REFERENCES users(id) ON DELETE SET NULL,

  -- A UPI reference, a bank reference, whatever the payer's app gave them.
  reference TEXT,

  UNIQUE (event_id, user_id)
);
CREATE INDEX idx_payments_owed ON payments(status, user_id);


-- Per-group append-only ledger. Balance = SUM(amount_pence), credits +, debits -.
CREATE TABLE kitty_entries (
  id           INTEGER PRIMARY KEY,
  group_id     INTEGER NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  type         TEXT NOT NULL CHECK (type IN ('payment','refund','adjustment','surplus')),
  amount_pence INTEGER NOT NULL,
  user_id      INTEGER REFERENCES users(id)  ON DELETE SET NULL,
  event_id     INTEGER REFERENCES events(id) ON DELETE SET NULL,
  note         TEXT,
  created_by   INTEGER REFERENCES users(id)  ON DELETE SET NULL,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_kitty_group ON kitty_entries(group_id, created_at);


-- A login link an admin hands to someone directly, usually pasted into WhatsApp.
-- This is the only way in for a member with no email, so for plain members it is
-- long-lived and reusable: it sits in their chat history and doubles as their
-- self-service recovery. Organiser links are single-use -- bigger blast radius.
CREATE TABLE claim_links (
  id          INTEGER PRIMARY KEY,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash  TEXT NOT NULL UNIQUE,
  single_use  INTEGER NOT NULL DEFAULT 0 CHECK (single_use IN (0,1)),
  expires_at  TEXT,
  consumed_at TEXT,
  created_by  INTEGER REFERENCES users(id) ON DELETE SET NULL,
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_claim_links_user ON claim_links(user_id);


-- Proving control of an email. One row carries both secrets, because one request
-- sends both: a tappable link and a typed code.
CREATE TABLE verification_challenges (
  id         INTEGER PRIMARY KEY,
  channel    TEXT NOT NULL DEFAULT 'email' CHECK (channel IN ('email')),
  identifier TEXT NOT NULL,                    -- normalized email

  link_token_hash TEXT NOT NULL UNIQUE,        -- 128-bit, no attempt limit needed
  code_hash       TEXT NOT NULL,               -- 6 digits; HMAC keyed with a server
                                               -- pepper and bound to identifier

  -- The device-user to merge, captured at REQUEST time from the cookie. Stamped
  -- here rather than read at open time because the link may be opened on a
  -- different device than it was requested from.
  claim_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,

  attempts     INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 5,     -- brute-force lockout for the code
  expires_at   TEXT NOT NULL,
  consumed_at  TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
-- Verification looks up by identifier, never by code hash alone.
CREATE INDEX idx_verif_lookup ON verification_challenges(identifier, consumed_at);


-- Background queue. The worker claims pending rows where run_at <= now inside a
-- transaction. Deliberately not river, which is Postgres-only.
CREATE TABLE jobs (
  id      INTEGER PRIMARY KEY,
  kind    TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}',
  run_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now')),
  status  TEXT NOT NULL DEFAULT 'pending'
            CHECK (status IN ('pending','claimed','done','failed')),
  attempts     INTEGER NOT NULL DEFAULT 0,
  claimed_at   TEXT,
  completed_at TEXT,
  last_error   TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_jobs_due ON jobs(status, run_at);


CREATE TABLE notifications (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type       TEXT NOT NULL,
  event_id   INTEGER REFERENCES events(id) ON DELETE CASCADE,
  title      TEXT NOT NULL,
  body       TEXT,
  read_at    TEXT,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_notifications_user ON notifications(user_id, created_at);


CREATE TABLE notification_preferences (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel    TEXT NOT NULL CHECK (channel IN ('inapp','email','webpush')),
  event_type TEXT NOT NULL DEFAULT 'all',
  enabled    INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  UNIQUE (user_id, channel, event_type)
);


CREATE TABLE push_subscriptions (
  id         INTEGER PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  endpoint   TEXT NOT NULL UNIQUE,
  p256dh     TEXT NOT NULL,
  auth       TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_push_user ON push_subscriptions(user_id);


CREATE TABLE audit_log (
  id          INTEGER PRIMARY KEY,
  actor_id    INTEGER REFERENCES users(id) ON DELETE SET NULL,
  action      TEXT NOT NULL,
  target_type TEXT,
  target_id   INTEGER,
  detail      TEXT NOT NULL DEFAULT '{}',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
CREATE INDEX idx_audit_actor ON audit_log(actor_id, created_at);
CREATE INDEX idx_audit_target ON audit_log(target_type, target_id);


-- Server-side sessions.
CREATE TABLE sessions (
  token  TEXT PRIMARY KEY,
  data   BLOB NOT NULL,
  expiry REAL NOT NULL
);
CREATE INDEX idx_sessions_expiry ON sessions(expiry);


-- +goose Down
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS audit_log;
DROP TABLE IF EXISTS push_subscriptions;
DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS verification_challenges;
DROP TABLE IF EXISTS claim_links;
DROP TABLE IF EXISTS kitty_entries;
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS sponsorships;
DROP TABLE IF EXISTS guests;
DROP TABLE IF EXISTS rsvps;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS group_members;
DROP TABLE IF EXISTS invites;
DROP TABLE IF EXISTS groups;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS community;
