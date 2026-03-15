import {
  Alert,
  Box,
  Button,
  Card,
  CardContent,
  CardHeader,
  Chip,
  FormControlLabel,
  Paper,
  Switch,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from '@mui/material';
import PlayArrowIcon from '@mui/icons-material/PlayArrow';
import StopIcon from '@mui/icons-material/Stop';
import ClearAllIcon from '@mui/icons-material/ClearAll';
import { useEffect, useRef, useState } from 'react';
import { useTelemetryStream } from '../api/hooks/useTelemetryStream';

export default function TelemetryPage() {
  const [deviceType, setDeviceType] = useState('');
  const [deviceIds, setDeviceIds] = useState('');
  const [batchSize, setBatchSize] = useState(50);
  const [autoScroll, setAutoScroll] = useState(true);

  const { events, connected, error, connect, disconnect, clearEvents } = useTelemetryStream();
  const bottomRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (autoScroll && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [events, autoScroll]);

  function handleConnect() {
    connect({
      device_type: deviceType || undefined,
      device_ids: deviceIds || undefined,
      batch_size: batchSize,
    });
  }

  function formatValue(v: number | string | boolean): string {
    if (typeof v === 'number') return v.toFixed(4);
    return String(v);
  }

  function formatTimestamp(ts: string): string {
    try {
      return new Date(ts).toLocaleTimeString(undefined, {
        hour12: false,
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        fractionalSecondDigits: 3,
      });
    } catch {
      return ts;
    }
  }

  return (
    <Box>
      <Typography variant="h5" fontWeight={700} mb={3}>
        Telemetry Stream
      </Typography>

      {/* Controls */}
      <Card sx={{ mb: 2 }}>
        <CardHeader
          title="Stream Controls"
          titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
          action={
            <Chip
              size="small"
              label={connected ? 'Connected' : 'Disconnected'}
              color={connected ? 'success' : 'default'}
              variant="outlined"
            />
          }
        />
        <CardContent>
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap', alignItems: 'flex-start' }}>
            <TextField
              label="Device Type"
              value={deviceType}
              onChange={(e) => setDeviceType(e.target.value)}
              size="small"
              placeholder="temperature_sensor"
              sx={{ minWidth: 200 }}
            />
            <TextField
              label="Device IDs"
              value={deviceIds}
              onChange={(e) => setDeviceIds(e.target.value)}
              size="small"
              placeholder="sensor-001,sensor-002"
              helperText="Comma-separated (optional)"
              sx={{ minWidth: 240 }}
            />
            <TextField
              label="Batch Size"
              type="number"
              value={batchSize}
              onChange={(e) =>
                setBatchSize(Math.min(500, Math.max(1, parseInt(e.target.value, 10) || 1)))
              }
              size="small"
              sx={{ width: 110 }}
              inputProps={{ min: 1, max: 500 }}
            />
            <Box sx={{ display: 'flex', gap: 1 }}>
              {!connected ? (
                <Button
                  variant="contained"
                  color="success"
                  startIcon={<PlayArrowIcon />}
                  onClick={handleConnect}
                >
                  Connect
                </Button>
              ) : (
                <Button
                  variant="outlined"
                  color="error"
                  startIcon={<StopIcon />}
                  onClick={disconnect}
                >
                  Disconnect
                </Button>
              )}
              <Button
                variant="outlined"
                startIcon={<ClearAllIcon />}
                onClick={clearEvents}
                disabled={events.length === 0}
              >
                Clear
              </Button>
            </Box>
          </Box>
        </CardContent>
      </Card>

      {error && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {error}
        </Alert>
      )}

      {/* Live events table */}
      <Card>
        <CardHeader
          title="Live Events"
          titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
          subheader={`${events.length} event${events.length !== 1 ? 's' : ''} (max 500)`}
          action={
            <FormControlLabel
              control={
                <Switch
                  size="small"
                  checked={autoScroll}
                  onChange={(e) => setAutoScroll(e.target.checked)}
                />
              }
              label={<Typography variant="caption">Auto-scroll</Typography>}
              labelPlacement="start"
            />
          }
        />
        <TableContainer
          component={Paper}
          elevation={0}
          sx={{ maxHeight: 520, bgcolor: 'transparent' }}
        >
          <Table stickyHeader size="small">
            <TableHead>
              <TableRow>
                <TableCell sx={{ width: 120 }}>Time</TableCell>
                <TableCell sx={{ width: 200 }}>Device ID</TableCell>
                <TableCell sx={{ width: 160 }}>Metric</TableCell>
                <TableCell>Value</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {events.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} align="center" sx={{ py: 6 }}>
                    <Typography color="text.secondary" variant="body2">
                      {connected
                        ? 'Waiting for telemetry data…'
                        : 'Connect to start receiving events'}
                    </Typography>
                  </TableCell>
                </TableRow>
              ) : (
                events.map((ev, i) => (
                  <TableRow key={i} hover>
                    <TableCell sx={{ fontFamily: 'monospace', fontSize: 12, whiteSpace: 'nowrap' }}>
                      {formatTimestamp(ev.timestamp)}
                    </TableCell>
                    <TableCell
                      sx={{ fontFamily: 'monospace', fontSize: 12, color: 'primary.light' }}
                    >
                      {ev.device_id}
                    </TableCell>
                    <TableCell sx={{ fontSize: 13 }}>{ev.metric}</TableCell>
                    <TableCell sx={{ fontWeight: 600, fontSize: 13 }}>
                      {formatValue(ev.value)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </TableContainer>
        <div ref={bottomRef} />
      </Card>
    </Box>
  );
}
