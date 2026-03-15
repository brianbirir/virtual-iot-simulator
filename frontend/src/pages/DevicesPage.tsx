import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  FormControlLabel,
  Grid,
  InputAdornment,
  List,
  ListItem,
  ListItemText,
  Radio,
  RadioGroup,
  Snackbar,
  TextField,
  Typography,
} from '@mui/material';
import { useState } from 'react';
import AddIcon from '@mui/icons-material/Add';
import StopIcon from '@mui/icons-material/Stop';
import { useSpawnDevices } from '../api/hooks/useSpawnDevices';
import { useStopDevices } from '../api/hooks/useStopDevices';
import { useStatus } from '../api/hooks/useStatus';

const STATE_COLORS: Record<string, 'success' | 'error' | 'warning' | 'info' | 'default'> = {
  RUNNING: 'success',
  STOPPED: 'default',
  ERROR: 'error',
  SPAWNING: 'info',
  PAUSED: 'warning',
};

interface SnackState {
  open: boolean;
  message: string;
  severity: 'success' | 'error';
}

export default function DevicesPage() {
  // Spawn form
  const [profile, setProfile] = useState('profiles/temperature_sensor.yaml');
  const [count, setCount] = useState(10);
  const [spawnRuntime, setSpawnRuntime] = useState('localhost:50051');

  // Stop form
  const [stopMode, setStopMode] = useState<'all' | 'type'>('all');
  const [deviceType, setDeviceType] = useState('');
  const [stopRuntime, setStopRuntime] = useState('localhost:50051');

  const [snack, setSnack] = useState<SnackState>({ open: false, message: '', severity: 'success' });

  const { data: status } = useStatus();
  const spawnMutation = useSpawnDevices();
  const stopMutation = useStopDevices();

  function showSnack(message: string, severity: 'success' | 'error') {
    setSnack({ open: true, message, severity });
  }

  async function handleSpawn() {
    try {
      const res = await spawnMutation.mutateAsync({
        profile,
        count,
        runtime: spawnRuntime || undefined,
      });
      const failMsg = res.failed.length ? `, ${res.failed.length} failed` : '';
      showSnack(`Spawned ${res.spawned} device(s)${failMsg}`, 'success');
    } catch (e) {
      showSnack((e as Error).message, 'error');
    }
  }

  async function handleStop() {
    try {
      const req =
        stopMode === 'all'
          ? { all_devices: true, runtime: stopRuntime || undefined }
          : { device_type: deviceType, runtime: stopRuntime || undefined };
      const res = await stopMutation.mutateAsync(req);
      showSnack(`Stopped ${res.stopped} device(s)`, 'success');
    } catch (e) {
      showSnack((e as Error).message, 'error');
    }
  }

  return (
    <Box>
      <Typography variant="h5" fontWeight={700} mb={3}>
        Devices
      </Typography>

      <Grid container spacing={2} mb={3}>
        {/* Spawn card */}
        <Grid item xs={12} md={6}>
          <Card>
            <CardHeader
              title="Spawn Devices"
              titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
            />
            <CardContent sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              <TextField
                label="Profile Path"
                value={profile}
                onChange={(e) => setProfile(e.target.value)}
                fullWidth
                size="small"
                placeholder="profiles/temperature_sensor.yaml"
                helperText="Relative path to the device YAML profile"
              />
              <TextField
                label="Device Count"
                type="number"
                value={count}
                onChange={(e) => setCount(Math.max(1, parseInt(e.target.value, 10) || 1))}
                fullWidth
                size="small"
                inputProps={{ min: 1 }}
              />
              <TextField
                label="Runtime Address"
                value={spawnRuntime}
                onChange={(e) => setSpawnRuntime(e.target.value)}
                fullWidth
                size="small"
                placeholder="localhost:50051"
                InputProps={{
                  endAdornment: (
                    <InputAdornment position="end">
                      <Typography variant="caption" color="text.secondary">
                        gRPC
                      </Typography>
                    </InputAdornment>
                  ),
                }}
              />
              <Button
                variant="contained"
                onClick={() => void handleSpawn()}
                disabled={spawnMutation.isPending || !profile}
                startIcon={
                  spawnMutation.isPending ? <CircularProgress size={16} color="inherit" /> : <AddIcon />
                }
              >
                {spawnMutation.isPending ? 'Spawning…' : 'Spawn Devices'}
              </Button>
            </CardContent>
          </Card>
        </Grid>

        {/* Stop card */}
        <Grid item xs={12} md={6}>
          <Card>
            <CardHeader
              title="Stop Devices"
              titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
            />
            <CardContent sx={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
              <RadioGroup
                row
                value={stopMode}
                onChange={(e) => setStopMode(e.target.value as 'all' | 'type')}
              >
                <FormControlLabel value="all" control={<Radio size="small" />} label="Stop all" />
                <FormControlLabel
                  value="type"
                  control={<Radio size="small" />}
                  label="By device type"
                />
              </RadioGroup>

              {stopMode === 'type' && (
                <TextField
                  label="Device Type"
                  value={deviceType}
                  onChange={(e) => setDeviceType(e.target.value)}
                  fullWidth
                  size="small"
                  placeholder="temperature_sensor"
                />
              )}

              <TextField
                label="Runtime Address"
                value={stopRuntime}
                onChange={(e) => setStopRuntime(e.target.value)}
                fullWidth
                size="small"
                placeholder="localhost:50051"
                InputProps={{
                  endAdornment: (
                    <InputAdornment position="end">
                      <Typography variant="caption" color="text.secondary">
                        gRPC
                      </Typography>
                    </InputAdornment>
                  ),
                }}
              />

              <Button
                variant="outlined"
                color="error"
                onClick={() => void handleStop()}
                disabled={stopMutation.isPending || (stopMode === 'type' && !deviceType)}
                startIcon={
                  stopMutation.isPending ? (
                    <CircularProgress size={16} color="inherit" />
                  ) : (
                    <StopIcon />
                  )
                }
              >
                {stopMutation.isPending ? 'Stopping…' : 'Stop Devices'}
              </Button>
            </CardContent>
          </Card>
        </Grid>
      </Grid>

      {/* Fleet summary */}
      <Card>
        <CardHeader
          title="Current Fleet"
          titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
          subheader={
            status
              ? `${status.fleet.total_devices} total device${status.fleet.total_devices !== 1 ? 's' : ''}`
              : undefined
          }
        />
        <CardContent>
          {!status && (
            <Typography color="text.secondary" variant="body2">
              Loading…
            </Typography>
          )}
          {status && (
            <Grid container spacing={3}>
              <Grid item xs={12} sm={6}>
                <Typography variant="body2" color="text.secondary" fontWeight={600} mb={1}>
                  By State
                </Typography>
                <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                  {Object.keys(status.fleet.by_state).length === 0 ? (
                    <Typography variant="body2" color="text.secondary">
                      No devices
                    </Typography>
                  ) : (
                    Object.entries(status.fleet.by_state).map(([state, c]) => (
                      <Chip
                        key={state}
                        label={`${state}: ${c}`}
                        color={STATE_COLORS[state] ?? 'default'}
                        size="small"
                      />
                    ))
                  )}
                </Box>
              </Grid>

              <Grid item xs={12} sm={6}>
                <Typography variant="body2" color="text.secondary" fontWeight={600} mb={1}>
                  By Type
                </Typography>
                {Object.keys(status.fleet.by_type).length === 0 ? (
                  <Typography variant="body2" color="text.secondary">
                    No devices
                  </Typography>
                ) : (
                  <List dense disablePadding>
                    {Object.entries(status.fleet.by_type).map(([type, c]) => (
                      <ListItem key={type} disablePadding sx={{ py: 0.5 }}>
                        <ListItemText
                          primary={type}
                          primaryTypographyProps={{ fontSize: 13 }}
                        />
                        <Chip label={c} size="small" variant="outlined" />
                      </ListItem>
                    ))}
                  </List>
                )}
              </Grid>
            </Grid>
          )}
        </CardContent>
      </Card>

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
