# Device Profiles

Profiles are YAML files in `profiles/` that define a device type's telemetry schema, protocol, and generator configuration.

```yaml
# profiles/temperature_sensor.yaml
type: temperature_sensor
protocol: console
topic_template: "devices/{device_id}/telemetry"
telemetry_interval: 5s
telemetry_fields:
  temperature:
    type: gaussian
    mean: 22.0
    stddev: 1.0
  humidity:
    type: gaussian
    mean: 55.0
    stddev: 5.0
  battery:
    type: static
    value: 100.0
labels:
  category: environmental
  firmware: "1.2.0"
```

## Top-level fields

| Field | Type | Required | Default | Description |
| ----- | ---- | :------: | ------- | ----------- |
| `type` | string | yes | — | Device type identifier (e.g. `temperature_sensor`). Automatically added as a `device_type` label on every spawned device. |
| `protocol` | string | no | `console` | Transport used to publish telemetry. One of: `mqtt`, `amqp`, `http`, `console`. |
| `topic_template` | string | no | `devices/{device_id}/telemetry` | Publish destination pattern. `{device_id}` is substituted at runtime. |
| `telemetry_interval` | string | no | `5s` | How often a telemetry reading is emitted. Accepts `ms`, `s`, `m`, `h` suffixes (e.g. `500ms`, `2m`). |
| `telemetry_fields` | map | no | `{}` | Map of field name → generator config. Each key becomes a metric in the telemetry payload. |
| `labels` | map | no | `{}` | Arbitrary `string: string` key/value pairs attached to every spawned device (useful for filtering and routing). |

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
| `value` | any | Constant value emitted on every tick. |
