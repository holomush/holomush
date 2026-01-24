# Operations Guide

This section covers deploying, configuring, and running HoloMUSH servers.

## Overview

HoloMUSH is designed to be easy to deploy and operate. It runs as a single binary
with PostgreSQL as its only external dependency.

## Getting Started

1. **Quick Start** - Get a development server running in minutes
2. **Production Setup** - Deploy with proper security and monitoring
3. **Configuration** - Customize behavior for your use case

## Documentation Sections

### Deployment

- [Quick Start](quickstart.md) - Run HoloMUSH locally for development
- [Production Deployment](deployment.md) - Deploy to Kubernetes or bare metal
- [Docker](docker.md) - Container images and compose files

### Configuration

- [Server Configuration](configuration.md) - All configuration options
- [TLS Setup](tls.md) - Secure connections with certificates
- [Database Setup](database.md) - PostgreSQL configuration

### Operations

- [Monitoring](monitoring.md) - Prometheus metrics and Grafana dashboards
- [Backup & Recovery](backup.md) - Data protection strategies
- [Scaling](scaling.md) - Horizontal scaling considerations

### Security

- [Security Hardening](security.md) - Best practices for production
- [Authentication](authentication.md) - Player authentication options
- [Access Control](access-control.md) - ABAC policy configuration

## Requirements

| Component  | Minimum | Recommended |
| ---------- | ------- | ----------- |
| CPU        | 1 core  | 2+ cores    |
| Memory     | 256 MB  | 512 MB      |
| PostgreSQL | 14+     | 16+         |
| Storage    | 1 GB    | 10+ GB      |

## Telnet vs Web

HoloMUSH supports two connection methods:

- **Telnet** (port 4201) - Classic MU* client compatibility
- **WebSocket** (port 8080) - Modern web client with PWA support

Both protocols connect to the same game world and share the same session system.
