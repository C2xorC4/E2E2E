-- Migration: Enhanced device profiles for hardware metrics and mesh networking
-- Stores detailed device information and network identifiers for P2P communication

-- Add detailed device profile columns
ALTER TABLE devices ADD COLUMN IF NOT EXISTS platform VARCHAR(20);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS os_version VARCHAR(50);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS manufacturer VARCHAR(100);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS model VARCHAR(100);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS hardware_id VARCHAR(255);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS screen_width INT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS screen_height INT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS cpu_arch VARCHAR(20);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS memory_mb INT;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS app_version VARCHAR(20);

-- Mesh networking identifiers
ALTER TABLE devices ADD COLUMN IF NOT EXISTS bluetooth_address VARCHAR(50);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS wifi_direct_address VARCHAR(50);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mesh_capable BOOLEAN DEFAULT FALSE;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mesh_enabled BOOLEAN DEFAULT FALSE;
ALTER TABLE devices ADD COLUMN IF NOT EXISTS mesh_discovery_id UUID;

-- Network state tracking
ALTER TABLE devices ADD COLUMN IF NOT EXISTS last_ip_address VARCHAR(45);
ALTER TABLE devices ADD COLUMN IF NOT EXISTS last_connection_type VARCHAR(20); -- wifi, cellular, ethernet, mesh

-- Create table for mesh network peers (discovered nearby devices)
CREATE TABLE IF NOT EXISTS mesh_peers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    peer_device_id UUID REFERENCES devices(id) ON DELETE SET NULL,
    peer_discovery_id UUID NOT NULL,
    peer_bluetooth_address VARCHAR(50),
    peer_wifi_direct_address VARCHAR(50),
    signal_strength INT,
    last_seen TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    connection_quality VARCHAR(20), -- excellent, good, fair, poor
    UNIQUE(device_id, peer_discovery_id)
);

CREATE INDEX IF NOT EXISTS idx_mesh_peers_device ON mesh_peers(device_id);
CREATE INDEX IF NOT EXISTS idx_mesh_peers_last_seen ON mesh_peers(last_seen);
CREATE INDEX IF NOT EXISTS idx_devices_mesh_discovery ON devices(mesh_discovery_id) WHERE mesh_discovery_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_devices_mesh_enabled ON devices(mesh_enabled) WHERE mesh_enabled = TRUE;

-- Device profile history for tracking changes over time
CREATE TABLE IF NOT EXISTS device_profile_history (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    device_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    changed_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    platform VARCHAR(20),
    os_version VARCHAR(50),
    app_version VARCHAR(20),
    ip_address VARCHAR(45),
    connection_type VARCHAR(20)
);

CREATE INDEX IF NOT EXISTS idx_device_history_device ON device_profile_history(device_id);
CREATE INDEX IF NOT EXISTS idx_device_history_time ON device_profile_history(changed_at DESC);

-- Function to log device profile changes
CREATE OR REPLACE FUNCTION log_device_profile_change()
RETURNS TRIGGER AS $$
BEGIN
    -- Log significant changes
    IF OLD.os_version IS DISTINCT FROM NEW.os_version
       OR OLD.app_version IS DISTINCT FROM NEW.app_version
       OR OLD.last_ip_address IS DISTINCT FROM NEW.last_ip_address
       OR OLD.last_connection_type IS DISTINCT FROM NEW.last_connection_type THEN
        INSERT INTO device_profile_history (device_id, platform, os_version, app_version, ip_address, connection_type)
        VALUES (NEW.id, NEW.platform, NEW.os_version, NEW.app_version, NEW.last_ip_address, NEW.last_connection_type);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger for profile history
DROP TRIGGER IF EXISTS trigger_device_profile_history ON devices;
CREATE TRIGGER trigger_device_profile_history
    AFTER UPDATE ON devices
    FOR EACH ROW
    EXECUTE FUNCTION log_device_profile_change();
