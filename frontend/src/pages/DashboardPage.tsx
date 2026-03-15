import {
  Box,
  Card,
  CardContent,
  CardHeader,
  Chip,
  CircularProgress,
  Grid,
  List,
  ListItem,
  ListItemText,
  Skeleton,
  Typography,
} from '@mui/material';
import { type ReactNode } from 'react';
import DevicesIcon from '@mui/icons-material/Devices';
import MemoryIcon from '@mui/icons-material/Memory';
import TimerIcon from '@mui/icons-material/Timer';
import CheckCircleIcon from '@mui/icons-material/CheckCircle';
import { useStatus } from '../api/hooks/useStatus';

const STATE_COLORS: Record<string, 'success' | 'error' | 'warning' | 'info' | 'default'> = {
  RUNNING: 'success',
  STOPPED: 'default',
  ERROR: 'error',
  SPAWNING: 'info',
  PAUSED: 'warning',
};

function formatUptime(seconds: number): string {
  if (seconds < 60) return `${Math.round(seconds)}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  return `${h}h ${m}m`;
}

function StatCard({
  title,
  value,
  icon,
  loading,
}: {
  title: string;
  value: string | number;
  icon: ReactNode;
  loading?: boolean;
}) {
  return (
    <Card>
      <CardContent>
        <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', mb: 1 }}>
          <Typography variant="body2" color="text.secondary">
            {title}
          </Typography>
          <Box sx={{ color: 'primary.main', display: 'flex' }}>{icon}</Box>
        </Box>
        {loading ? (
          <Skeleton width={80} height={40} />
        ) : (
          <Typography variant="h4" fontWeight={700}>
            {value}
          </Typography>
        )}
      </CardContent>
    </Card>
  );
}

export default function DashboardPage() {
  const { data, isLoading, isError } = useStatus();
  const fleet = data?.fleet;
  const runtime = data?.runtime;

  return (
    <Box>
      <Typography variant="h5" fontWeight={700} mb={3}>
        Dashboard
      </Typography>

      {isError && (
        <Typography color="error" mb={2}>
          Failed to load status. Is the orchestrator running?
        </Typography>
      )}

      {/* Stat cards row */}
      <Grid container spacing={2} mb={3}>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title="Total Devices"
            value={fleet?.total_devices ?? 0}
            icon={<DevicesIcon />}
            loading={isLoading}
          />
        </Grid>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title="Active Devices"
            value={runtime?.active_devices ?? 0}
            icon={<CheckCircleIcon />}
            loading={isLoading}
          />
        </Grid>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title="Runtime Memory"
            value={runtime ? `${runtime.memory_mb.toFixed(1)} MB` : '—'}
            icon={<MemoryIcon />}
            loading={isLoading}
          />
        </Grid>
        <Grid item xs={12} sm={6} lg={3}>
          <StatCard
            title="Uptime"
            value={runtime ? formatUptime(runtime.uptime_seconds) : '—'}
            icon={<TimerIcon />}
            loading={isLoading}
          />
        </Grid>
      </Grid>

      {/* Detail cards row */}
      <Grid container spacing={2}>
        {/* Fleet by state */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardHeader
              title="Fleet by State"
              titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
            />
            <CardContent>
              {isLoading && <CircularProgress size={24} />}
              {fleet && Object.keys(fleet.by_state).length === 0 && (
                <Typography color="text.secondary" variant="body2">
                  No devices running
                </Typography>
              )}
              {fleet && Object.keys(fleet.by_state).length > 0 && (
                <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                  {Object.entries(fleet.by_state).map(([state, count]) => (
                    <Chip
                      key={state}
                      label={`${state}: ${count}`}
                      color={STATE_COLORS[state] ?? 'default'}
                      size="small"
                    />
                  ))}
                </Box>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Fleet by type */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardHeader
              title="Fleet by Type"
              titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
            />
            <CardContent>
              {isLoading && <CircularProgress size={24} />}
              {fleet && Object.keys(fleet.by_type).length === 0 && (
                <Typography color="text.secondary" variant="body2">
                  No devices running
                </Typography>
              )}
              {fleet && Object.keys(fleet.by_type).length > 0 && (
                <List dense disablePadding>
                  {Object.entries(fleet.by_type).map(([type, count]) => (
                    <ListItem key={type} disablePadding sx={{ py: 0.5 }}>
                      <ListItemText
                        primary={type}
                        primaryTypographyProps={{ fontSize: 13 }}
                      />
                      <Chip label={count} size="small" variant="outlined" />
                    </ListItem>
                  ))}
                </List>
              )}
            </CardContent>
          </Card>
        </Grid>

        {/* Runtime details */}
        <Grid item xs={12} md={4}>
          <Card sx={{ height: '100%' }}>
            <CardHeader
              title="Runtime Details"
              titleTypographyProps={{ variant: 'subtitle1', fontWeight: 700 }}
            />
            <CardContent>
              {isLoading && <CircularProgress size={24} />}
              {runtime && (
                <List dense disablePadding>
                  {[
                    { label: 'Active Devices', value: runtime.active_devices },
                    { label: 'Goroutines', value: runtime.goroutine_count },
                    { label: 'Memory', value: `${runtime.memory_mb.toFixed(2)} MB` },
                    { label: 'Uptime', value: formatUptime(runtime.uptime_seconds) },
                  ].map(({ label, value }) => (
                    <ListItem
                      key={label}
                      disablePadding
                      sx={{ py: 0.75, borderBottom: '1px solid rgba(255,255,255,0.04)' }}
                    >
                      <ListItemText
                        primary={label}
                        primaryTypographyProps={{ fontSize: 13, color: 'text.secondary' }}
                      />
                      <Typography variant="body2" fontWeight={600}>
                        {value}
                      </Typography>
                    </ListItem>
                  ))}
                </List>
              )}
            </CardContent>
          </Card>
        </Grid>
      </Grid>
    </Box>
  );
}
