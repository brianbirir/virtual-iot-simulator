import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  FormControl,
  Grid2 as Grid,
  IconButton,
  InputLabel,
  MenuItem,
  Select,
  Snackbar,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import DeleteIcon from '@mui/icons-material/Delete';
import EditIcon from '@mui/icons-material/Edit';
import AddCircleOutlineIcon from '@mui/icons-material/AddCircleOutline';
import RemoveCircleOutlineIcon from '@mui/icons-material/RemoveCircleOutline';
import { useState } from 'react';
import { useProfiles } from '../api/hooks/useProfiles';
import { useCreateProfile } from '../api/hooks/useCreateProfile';
import { useUpdateProfile } from '../api/hooks/useUpdateProfile';
import { useDeleteProfile } from '../api/hooks/useDeleteProfile';
import type { DeviceProfile, GeneratorType, Protocol, TelemetryFieldConfig } from '../api/types';

// ── Constants ────────────────────────────────────────────────────────────────

const PROTOCOLS: Protocol[] = ['console', 'mqtt', 'amqp', 'http'];
const GENERATOR_TYPES: GeneratorType[] = ['gaussian', 'static', 'brownian', 'diurnal', 'markov'];

// ── Form state ───────────────────────────────────────────────────────────────

interface FieldRow {
  name: string;
  config: TelemetryFieldConfig;
}

interface FormState {
  name: string;
  type: string;
  protocol: Protocol;
  topic_template: string;
  telemetry_interval: string;
  fields: FieldRow[];
  labelKeys: string[];
  labelValues: string[];
}

function emptyForm(): FormState {
  return {
    name: '',
    type: '',
    protocol: 'console',
    topic_template: 'devices/{device_id}/telemetry',
    telemetry_interval: '5s',
    fields: [],
    labelKeys: [],
    labelValues: [],
  };
}

function profileToForm(p: DeviceProfile): FormState {
  const fields: FieldRow[] = Object.entries(p.telemetry_fields).map(([name, config]) => ({
    name,
    config,
  }));
  const labelKeys = Object.keys(p.labels);
  const labelValues = Object.values(p.labels);
  return {
    name: p.name,
    type: p.type,
    protocol: p.protocol,
    topic_template: p.topic_template,
    telemetry_interval: p.telemetry_interval,
    fields,
    labelKeys,
    labelValues,
  };
}

function formToPayload(form: FormState) {
  const telemetry_fields: Record<string, TelemetryFieldConfig> = {};
  for (const row of form.fields) {
    if (row.name.trim()) {
      telemetry_fields[row.name.trim()] = row.config;
    }
  }
  const labels: Record<string, string> = {};
  for (let i = 0; i < form.labelKeys.length; i++) {
    const k = form.labelKeys[i]?.trim();
    if (k) labels[k] = form.labelValues[i] ?? '';
  }
  return {
    name: form.name.trim(),
    type: form.type.trim(),
    protocol: form.protocol,
    topic_template: form.topic_template.trim(),
    telemetry_interval: form.telemetry_interval.trim(),
    telemetry_fields,
    labels,
  };
}

// ── Generator config fields per type ─────────────────────────────────────────

function GeneratorFields({
  config,
  onChange,
}: {
  config: TelemetryFieldConfig;
  onChange: (updated: TelemetryFieldConfig) => void;
}) {
  function num(key: keyof TelemetryFieldConfig, label: string) {
    return (
      <TextField
        key={key}
        label={label}
        type="number"
        size="small"
        value={(config[key] as number | undefined) ?? ''}
        onChange={(e) =>
          onChange({ ...config, [key]: e.target.value === '' ? undefined : Number(e.target.value) })
        }
        sx={{ flex: 1, minWidth: 100 }}
      />
    );
  }

  switch (config.type) {
    case 'gaussian':
      return (
        <Stack direction="row" spacing={1} flexWrap="wrap">
          {num('mean', 'Mean')}
          {num('stddev', 'Std Dev')}
        </Stack>
      );
    case 'static':
      return <Stack direction="row">{num('value', 'Value')}</Stack>;
    case 'brownian':
      return (
        <Stack direction="row" spacing={1} flexWrap="wrap">
          {num('start', 'Start')}
          {num('drift', 'Drift')}
          {num('volatility', 'Volatility')}
          {num('mean_reversion', 'Mean Reversion')}
          {num('min', 'Min')}
          {num('max', 'Max')}
        </Stack>
      );
    case 'diurnal':
      return (
        <Stack direction="row" spacing={1} flexWrap="wrap">
          {num('baseline', 'Baseline')}
          {num('amplitude', 'Amplitude')}
          {num('peak_hour', 'Peak Hour')}
          {num('noise_stddev', 'Noise StdDev')}
        </Stack>
      );
    case 'markov':
      return (
        <TextField
          label="States (comma-separated)"
          size="small"
          fullWidth
          value={(config.states ?? []).join(', ')}
          onChange={(e) =>
            onChange({
              ...config,
              states: e.target.value.split(',').map((s) => s.trim()).filter(Boolean),
            })
          }
          helperText="Transition matrix editing not yet supported in UI — set via API"
        />
      );
    default:
      return null;
  }
}

// ── Profile form dialog ───────────────────────────────────────────────────────

function ProfileFormDialog({
  open,
  title,
  initial,
  saving,
  onSave,
  onClose,
}: {
  open: boolean;
  title: string;
  initial: FormState;
  saving: boolean;
  onSave: (payload: ReturnType<typeof formToPayload>) => void;
  onClose: () => void;
}) {
  const [form, setForm] = useState<FormState>(initial);

  // Reset when dialog opens with new initial value
  const [lastInitial, setLastInitial] = useState(initial);
  if (initial !== lastInitial) {
    setForm(initial);
    setLastInitial(initial);
  }

  function addField() {
    setForm((f) => ({
      ...f,
      fields: [...f.fields, { name: '', config: { type: 'gaussian', mean: 0, stddev: 1 } }],
    }));
  }

  function removeField(i: number) {
    setForm((f) => ({ ...f, fields: f.fields.filter((_, idx) => idx !== i) }));
  }

  function updateField(i: number, updated: FieldRow) {
    setForm((f) => ({ ...f, fields: f.fields.map((r, idx) => (idx === i ? updated : r)) }));
  }

  function addLabel() {
    setForm((f) => ({
      ...f,
      labelKeys: [...f.labelKeys, ''],
      labelValues: [...f.labelValues, ''],
    }));
  }

  function removeLabel(i: number) {
    setForm((f) => ({
      ...f,
      labelKeys: f.labelKeys.filter((_, idx) => idx !== i),
      labelValues: f.labelValues.filter((_, idx) => idx !== i),
    }));
  }

  const isValid = form.name.trim() && form.type.trim() && form.telemetry_interval.trim();

  return (
    <Dialog open={open} onClose={onClose} maxWidth="md" fullWidth>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3} sx={{ pt: 1 }}>
          {/* Basic info */}
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, sm: 6 }}>
              <TextField
                label="Profile Name"
                value={form.name}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
                fullWidth
                size="small"
                required
                helperText="Unique human-readable name"
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 6 }}>
              <TextField
                label="Device Type"
                value={form.type}
                onChange={(e) => setForm((f) => ({ ...f, type: e.target.value }))}
                fullWidth
                size="small"
                required
                helperText="e.g. temperature_sensor"
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <FormControl size="small" fullWidth>
                <InputLabel>Protocol</InputLabel>
                <Select
                  label="Protocol"
                  value={form.protocol}
                  onChange={(e) => setForm((f) => ({ ...f, protocol: e.target.value as Protocol }))}
                >
                  {PROTOCOLS.map((p) => (
                    <MenuItem key={p} value={p}>
                      {p}
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <TextField
                label="Telemetry Interval"
                value={form.telemetry_interval}
                onChange={(e) => setForm((f) => ({ ...f, telemetry_interval: e.target.value }))}
                fullWidth
                size="small"
                required
                helperText="e.g. 5s, 500ms, 1m"
              />
            </Grid>
            <Grid size={{ xs: 12, sm: 4 }}>
              <TextField
                label="Topic Template"
                value={form.topic_template}
                onChange={(e) => setForm((f) => ({ ...f, topic_template: e.target.value }))}
                fullWidth
                size="small"
              />
            </Grid>
          </Grid>

          <Divider />

          {/* Telemetry fields */}
          <Box>
            <Stack direction="row" alignItems="center" justifyContent="space-between" mb={1}>
              <Typography variant="subtitle2" fontWeight={700}>
                Telemetry Fields
              </Typography>
              <Button size="small" startIcon={<AddCircleOutlineIcon />} onClick={addField}>
                Add Field
              </Button>
            </Stack>
            <Stack spacing={2}>
              {form.fields.length === 0 && (
                <Typography variant="body2" color="text.secondary">
                  No fields — click "Add Field" to define telemetry output.
                </Typography>
              )}
              {form.fields.map((row, i) => (
                <Card key={i} variant="outlined" sx={{ p: 1.5 }}>
                  <Stack spacing={1.5}>
                    <Stack direction="row" spacing={1} alignItems="center">
                      <TextField
                        label="Field Name"
                        size="small"
                        value={row.name}
                        onChange={(e) => updateField(i, { ...row, name: e.target.value })}
                        sx={{ flex: 1 }}
                        placeholder="e.g. temperature"
                      />
                      <FormControl size="small" sx={{ minWidth: 130 }}>
                        <InputLabel>Generator</InputLabel>
                        <Select
                          label="Generator"
                          value={row.config.type}
                          onChange={(e) =>
                            updateField(i, {
                              ...row,
                              config: { type: e.target.value as GeneratorType },
                            })
                          }
                        >
                          {GENERATOR_TYPES.map((t) => (
                            <MenuItem key={t} value={t}>
                              {t}
                            </MenuItem>
                          ))}
                        </Select>
                      </FormControl>
                      <Tooltip title="Remove field">
                        <IconButton size="small" color="error" onClick={() => removeField(i)}>
                          <RemoveCircleOutlineIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </Stack>
                    <GeneratorFields
                      config={row.config}
                      onChange={(updated) => updateField(i, { ...row, config: updated })}
                    />
                  </Stack>
                </Card>
              ))}
            </Stack>
          </Box>

          <Divider />

          {/* Labels */}
          <Box>
            <Stack direction="row" alignItems="center" justifyContent="space-between" mb={1}>
              <Typography variant="subtitle2" fontWeight={700}>
                Labels
              </Typography>
              <Button size="small" startIcon={<AddCircleOutlineIcon />} onClick={addLabel}>
                Add Label
              </Button>
            </Stack>
            <Stack spacing={1}>
              {form.labelKeys.length === 0 && (
                <Typography variant="body2" color="text.secondary">
                  No labels — click "Add Label" to attach metadata.
                </Typography>
              )}
              {form.labelKeys.map((key, i) => (
                <Stack key={i} direction="row" spacing={1} alignItems="center">
                  <TextField
                    label="Key"
                    size="small"
                    value={key}
                    onChange={(e) =>
                      setForm((f) => ({
                        ...f,
                        labelKeys: f.labelKeys.map((k, idx) => (idx === i ? e.target.value : k)),
                      }))
                    }
                    sx={{ flex: 1 }}
                  />
                  <TextField
                    label="Value"
                    size="small"
                    value={form.labelValues[i] ?? ''}
                    onChange={(e) =>
                      setForm((f) => ({
                        ...f,
                        labelValues: f.labelValues.map((v, idx) =>
                          idx === i ? e.target.value : v,
                        ),
                      }))
                    }
                    sx={{ flex: 1 }}
                  />
                  <Tooltip title="Remove label">
                    <IconButton size="small" color="error" onClick={() => removeLabel(i)}>
                      <RemoveCircleOutlineIcon fontSize="small" />
                    </IconButton>
                  </Tooltip>
                </Stack>
              ))}
            </Stack>
          </Box>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={saving}>
          Cancel
        </Button>
        <Button
          variant="contained"
          onClick={() => onSave(formToPayload(form))}
          disabled={saving || !isValid}
          startIcon={saving ? <CircularProgress size={14} color="inherit" /> : undefined}
        >
          {saving ? 'Saving…' : 'Save'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

// ── Delete confirm dialog ─────────────────────────────────────────────────────

function DeleteConfirmDialog({
  profile,
  deleting,
  onConfirm,
  onClose,
}: {
  profile: DeviceProfile | null;
  deleting: boolean;
  onConfirm: () => void;
  onClose: () => void;
}) {
  return (
    <Dialog open={!!profile} onClose={onClose} maxWidth="xs" fullWidth>
      <DialogTitle>Delete Profile</DialogTitle>
      <DialogContent>
        <Typography>
          Delete <strong>{profile?.name}</strong>? This cannot be undone.
        </Typography>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose} disabled={deleting}>
          Cancel
        </Button>
        <Button
          variant="contained"
          color="error"
          onClick={onConfirm}
          disabled={deleting}
          startIcon={deleting ? <CircularProgress size={14} color="inherit" /> : undefined}
        >
          {deleting ? 'Deleting…' : 'Delete'}
        </Button>
      </DialogActions>
    </Dialog>
  );
}

// ── Main page ─────────────────────────────────────────────────────────────────

interface SnackState {
  open: boolean;
  message: string;
  severity: 'success' | 'error';
}

export default function ProfilesPage() {
  const { data: profiles, isLoading } = useProfiles();
  const createMutation = useCreateProfile();
  const updateMutation = useUpdateProfile();
  const deleteMutation = useDeleteProfile();

  const [createOpen, setCreateOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<DeviceProfile | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<DeviceProfile | null>(null);
  const [snack, setSnack] = useState<SnackState>({ open: false, message: '', severity: 'success' });

  function showSnack(message: string, severity: 'success' | 'error') {
    setSnack({ open: true, message, severity });
  }

  async function handleCreate(payload: ReturnType<typeof formToPayload>) {
    try {
      await createMutation.mutateAsync(payload);
      setCreateOpen(false);
      showSnack(`Profile "${payload.name}" created`, 'success');
    } catch (e) {
      showSnack((e as Error).message, 'error');
    }
  }

  async function handleUpdate(payload: ReturnType<typeof formToPayload>) {
    if (!editTarget) return;
    try {
      await updateMutation.mutateAsync({ id: editTarget.id, body: payload });
      setEditTarget(null);
      showSnack(`Profile "${payload.name}" updated`, 'success');
    } catch (e) {
      showSnack((e as Error).message, 'error');
    }
  }

  async function handleDelete() {
    if (!deleteTarget) return;
    try {
      await deleteMutation.mutateAsync(deleteTarget.id);
      setDeleteTarget(null);
      showSnack(`Profile "${deleteTarget.name}" deleted`, 'success');
    } catch (e) {
      showSnack((e as Error).message, 'error');
    }
  }

  return (
    <Box>
      <Stack direction="row" alignItems="center" justifyContent="space-between" mb={3}>
        <Typography variant="h5" fontWeight={700}>
          Device Profiles
        </Typography>
        <Button variant="contained" startIcon={<AddIcon />} onClick={() => setCreateOpen(true)}>
          New Profile
        </Button>
      </Stack>

      <Card>
        <CardHeader
          title="Profiles"
          slotProps={{ title: { variant: 'subtitle1', fontWeight: 700 } }}
          subheader={profiles ? `${profiles.length} profile${profiles.length !== 1 ? 's' : ''}` : undefined}
        />
        <CardContent sx={{ p: 0 }}>
          {isLoading && (
            <Box sx={{ p: 3, textAlign: 'center' }}>
              <CircularProgress size={24} />
            </Box>
          )}
          {!isLoading && (!profiles || profiles.length === 0) && (
            <Typography variant="body2" color="text.secondary" sx={{ p: 3 }}>
              No profiles yet. Click "New Profile" to create one.
            </Typography>
          )}
          {profiles && profiles.length > 0 && (
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Name</TableCell>
                  <TableCell>Type</TableCell>
                  <TableCell>Protocol</TableCell>
                  <TableCell>Interval</TableCell>
                  <TableCell>Fields</TableCell>
                  <TableCell>Labels</TableCell>
                  <TableCell align="right">Actions</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {profiles.map((p) => (
                  <TableRow key={p.id} hover>
                    <TableCell>
                      <Typography variant="body2" fontWeight={600}>
                        {p.name}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Chip label={p.type} size="small" variant="outlined" />
                    </TableCell>
                    <TableCell>{p.protocol}</TableCell>
                    <TableCell>{p.telemetry_interval}</TableCell>
                    <TableCell>{Object.keys(p.telemetry_fields).join(', ') || '—'}</TableCell>
                    <TableCell>
                      {Object.keys(p.labels).length === 0
                        ? '—'
                        : Object.entries(p.labels)
                            .map(([k, v]) => `${k}=${v}`)
                            .join(', ')}
                    </TableCell>
                    <TableCell align="right">
                      <Stack direction="row" spacing={0.5} justifyContent="flex-end">
                        <Tooltip title="Edit">
                          <IconButton size="small" onClick={() => setEditTarget(p)}>
                            <EditIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                        <Tooltip title="Delete">
                          <IconButton size="small" color="error" onClick={() => setDeleteTarget(p)}>
                            <DeleteIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      </Stack>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* Create dialog */}
      <ProfileFormDialog
        open={createOpen}
        title="New Device Profile"
        initial={emptyForm()}
        saving={createMutation.isPending}
        onSave={handleCreate}
        onClose={() => setCreateOpen(false)}
      />

      {/* Edit dialog */}
      <ProfileFormDialog
        open={!!editTarget}
        title={`Edit: ${editTarget?.name ?? ''}`}
        initial={editTarget ? profileToForm(editTarget) : emptyForm()}
        saving={updateMutation.isPending}
        onSave={handleUpdate}
        onClose={() => setEditTarget(null)}
      />

      {/* Delete confirm dialog */}
      <DeleteConfirmDialog
        profile={deleteTarget}
        deleting={deleteMutation.isPending}
        onConfirm={() => void handleDelete()}
        onClose={() => setDeleteTarget(null)}
      />

      <Snackbar
        open={snack.open}
        autoHideDuration={4000}
        onClose={() => setSnack((s) => ({ ...s, open: false }))}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert
          severity={snack.severity}
          onClose={() => setSnack((s) => ({ ...s, open: false }))}
          variant="filled"
        >
          {snack.message}
        </Alert>
      </Snackbar>
    </Box>
  );
}
