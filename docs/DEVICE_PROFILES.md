# Device Profiles

Device profiles define a device type's telemetry schema, protocol, and generator configuration. Profiles are persisted in **PostgreSQL** and managed through the REST API or the **Profiles** page in the web dashboard.

## Profile fields

| Field | Type | Required | Default | Description |
| ----- | ---- | :------: | ------- | ----------- |
| `name` | string | yes | — | Unique human-readable identifier (e.g. `temperature-sensor-v1`). Used to select profiles in the UI and API. |
| `type` | string | yes | — | Device type identifier (e.g. `temperature_sensor`). Automatically added as a `device_type` label on every spawned device. |
| `protocol` | string | no | `console` | Transport used to publish telemetry. One of: `mqtt`, `amqp`, `http`, `console`. |
| `topic_template` | string | no | `devices/{device_id}/telemetry` | Publish destination pattern. `{device_id}` is substituted at runtime. |
| `telemetry_interval` | string | no | `5s` | How often a telemetry reading is emitted. Accepts `ms`, `s`, `m`, `h` suffixes (e.g. `500ms`, `2m`). |
| `telemetry_fields` | object | no | `{}` | Map of field name → generator config. Each key becomes a metric in the telemetry payload. |
| `labels` | object | no | `{}` | Arbitrary `string: string` key/value pairs attached to every spawned device (useful for filtering and routing). |

Two read-only timestamps are also stored: `created_at` and `updated_at`.

## Example profile (JSON)

```json
{
  "name": "temperature-sensor-v1",
  "type": "temperature_sensor",
  "protocol": "console",
  "topic_template": "devices/{device_id}/telemetry",
  "telemetry_interval": "5s",
  "telemetry_fields": {
    "temperature": { "type": "gaussian", "mean": 22.0, "stddev": 1.0 },
    "humidity":    { "type": "gaussian", "mean": 55.0, "stddev": 5.0 },
    "battery":     { "type": "static",   "value": 100.0 }
  },
  "labels": {
    "category": "environmental",
    "firmware": "1.2.0"
  }
}
```

## Generator types (`telemetry_fields[*].type`)

Every field entry requires a `type` key. The remaining parameters depend on the chosen generator.

### `gaussian` — normally distributed sample

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| `mean` | float | Centre of the distribution. |
| `stddev` | float | Standard deviation. |

### `brownian` — Ornstein-Uhlenbeck random walk

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| `start` | float | Initial value. |
| `drift` | float | Constant drift added each tick. |
| `volatility` | float | Scale of the random step (higher = noisier). |
| `mean_reversion` | float | Pull strength back toward `start` (0 = pure random walk). |
| `min` | float | Optional lower clamp. |
| `max` | float | Optional upper clamp. |

### `diurnal` — sinusoidal day/night cycle

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| `baseline` | float | Midpoint value around which the signal oscillates. |
| `amplitude` | float | Peak deviation from `baseline`. |
| `peak_hour` | int | Hour of day (0–23) at which the signal reaches its maximum. |
| `noise_stddev` | float | Gaussian noise added on each tick. |

### `markov` — discrete state machine

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| `states` | list[string] | Ordered list of state names (e.g. `["ok", "warn", "error"]`). |
| `transition_matrix` | list[list[float]] | Row-stochastic matrix of transition probabilities. Row *i* column *j* is the probability of moving from state *i* to state *j*. |
| `initial_state` | string | Starting state; must be present in `states`. |

### `static` — fixed value

| Parameter | Type | Description |
| --------- | ---- | ----------- |
| `value` | number | Constant value emitted on every tick. |

---

## Managing profiles via API

The base URL for all profile endpoints is `/api/v1/profiles`. Interactive API documentation is available at `/docs` when the orchestrator is running.

### List profiles

```http
GET /api/v1/profiles
```

Returns an array of all profiles ordered by name.

### Create a profile

```http
POST /api/v1/profiles
Content-Type: application/json

{
  "name": "temperature-sensor-v1",
  "type": "temperature_sensor",
  "protocol": "console",
  "telemetry_interval": "5s",
  "telemetry_fields": {
    "temperature": { "type": "gaussian", "mean": 22.0, "stddev": 1.0 }
  },
  "labels": {}
}
```

Returns `201 Created` with the full profile object including its `id`.

### Get a profile

```http
GET /api/v1/profiles/{id}
```

### Update a profile

```http
PUT /api/v1/profiles/{id}
Content-Type: application/json

{ "telemetry_interval": "10s" }
```

Only the fields included in the request body are updated (all fields are optional).

### Delete a profile

```http
DELETE /api/v1/profiles/{id}
```

Returns `204 No Content`.

---

## Spawning devices from a profile

Pass the profile `id` (UUID) in the spawn request:

```http
POST /api/v1/devices/spawn
Content-Type: application/json

{
  "profile_id": "d1e2f3a4-...",
  "count": 10
}
```

The orchestrator loads the profile from PostgreSQL, validates it, generates `DeviceSpec` messages, and forwards them to the device runtime over gRPC.

---

## Managing profiles in the dashboard

The **Profiles** page (`/profiles`) provides a full CRUD interface:

- **Profile table** — lists all profiles with their type, protocol, interval, field names, and labels.
- **New Profile button** — opens a form with fields for all profile properties. Telemetry fields can be added one at a time, each with a generator type selector and the corresponding parameters. Labels are entered as key/value pairs.
- **Edit (pencil icon)** — opens the same form pre-populated with the profile's current values.
- **Delete (trash icon)** — shows a confirmation dialog before permanently removing the profile.

On the **Devices** page, the "Spawn Devices" form shows a dropdown of profiles loaded from the database instead of a file-path text field.
