import {
  AppBar,
  Box,
  Chip,
  CssBaseline,
  Divider,
  Drawer,
  List,
  ListItem,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Toolbar,
  Typography,
} from '@mui/material';
import DashboardIcon from '@mui/icons-material/Dashboard';
import DevicesIcon from '@mui/icons-material/Devices';
import TimelineIcon from '@mui/icons-material/Timeline';
import RouterIcon from '@mui/icons-material/Router';
import TuneIcon from '@mui/icons-material/Tune';
import { type ReactNode } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { useHealth } from '../../api/hooks/useHealth';

const DRAWER_WIDTH = 220;

const NAV_ITEMS = [
  { label: 'Dashboard', path: '/', icon: <DashboardIcon fontSize="small" /> },
  { label: 'Devices', path: '/devices', icon: <DevicesIcon fontSize="small" /> },
  { label: 'Profiles', path: '/profiles', icon: <TuneIcon fontSize="small" /> },
  { label: 'Telemetry', path: '/telemetry', icon: <TimelineIcon fontSize="small" /> },
];

export default function AppLayout({ children }: { children: ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const { data: health, isError } = useHealth();

  const healthOk = !isError && health?.status === 'ok';

  return (
    <Box sx={{ display: 'flex' }}>
      <CssBaseline />

      <AppBar
        position="fixed"
        elevation={0}
        sx={{
          width: `calc(100% - ${DRAWER_WIDTH}px)`,
          ml: `${DRAWER_WIDTH}px`,
          borderBottom: '1px solid rgba(255,255,255,0.08)',
          bgcolor: 'background.paper',
        }}
      >
        <Toolbar sx={{ justifyContent: 'space-between' }}>
          <Typography variant="h6" sx={{ fontWeight: 700, letterSpacing: 0.5 }}>
            Virtual IoT Simulator
          </Typography>
          <Chip
            size="small"
            label={healthOk ? 'API Online' : 'API Offline'}
            color={healthOk ? 'success' : 'error'}
            variant="outlined"
          />
        </Toolbar>
      </AppBar>

      <Drawer
        variant="permanent"
        sx={{
          width: DRAWER_WIDTH,
          flexShrink: 0,
          '& .MuiDrawer-paper': {
            width: DRAWER_WIDTH,
            boxSizing: 'border-box',
            bgcolor: 'background.paper',
            borderRight: '1px solid rgba(255,255,255,0.08)',
          },
        }}
      >
        <Toolbar sx={{ gap: 1 }}>
          <RouterIcon color="primary" />
          <Typography variant="subtitle1" fontWeight={700} color="primary">
            IoT Sim
          </Typography>
        </Toolbar>
        <Divider />
        <List sx={{ pt: 1, px: 1 }}>
          {NAV_ITEMS.map((item) => {
            const active = location.pathname === item.path;
            return (
              <ListItem key={item.path} disablePadding sx={{ mb: 0.5 }}>
                <ListItemButton
                  selected={active}
                  onClick={() => void navigate(item.path)}
                  sx={{ borderRadius: 1 }}
                >
                  <ListItemIcon
                    sx={{ minWidth: 36, color: active ? 'primary.main' : 'text.secondary' }}
                  >
                    {item.icon}
                  </ListItemIcon>
                  <ListItemText
                    primary={item.label}
                    primaryTypographyProps={{
                      fontSize: 14,
                      fontWeight: active ? 700 : 400,
                    }}
                  />
                </ListItemButton>
              </ListItem>
            );
          })}
        </List>
      </Drawer>

      <Box
        component="main"
        sx={{
          flexGrow: 1,
          p: 3,
          mt: 8,
          minHeight: '100vh',
          bgcolor: 'background.default',
        }}
      >
        {children}
      </Box>
    </Box>
  );
}
