# Security Policy

## Overview

KrakenD MCP Server is an MCP (Model Context Protocol) server that runs locally on your machine to provide AI assistance for KrakenD configuration. Security is a shared responsibility between this project and your local environment setup.

## Supported Versions

| Version | Supported          | Notes                           |
| ------- | ------------------ | ------------------------------- |
| 0.6.x   | :white_check_mark: | Current stable release          |
| < 0.6.0 | :x:                | Please upgrade to latest version|

We recommend always using the latest version from the `main` branch for the most up-to-date security fixes.

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

KrakenD takes cybersecurity seriously and follows a responsible disclosure process. We appreciate the security research community's help in keeping our software secure.

### How to Report

**Email**: [email protected]
- Include "KrakenD MCP Server Security" in the subject line
- Provide detailed information about the vulnerability
- KrakenD will acknowledge receipt and provide next steps

### What to Include

When reporting a vulnerability, please include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if available)
- Your contact information
- Whether you want to be credited for the discovery

### Responsible Disclosure

We ask security researchers to:
- ✅ **Do** allow KrakenD time to investigate and fix the issue
- ✅ **Do** provide detailed reproduction steps
- ✅ **Do** work with us to understand the impact
- ❌ **Don't** publish vulnerabilities before fixes are released
- ❌ **Don't** divulge exploit details or proof-of-concept code publicly
- ❌ **Don't** test against production systems

### Response Process

1. **Acknowledgment**: KrakenD acknowledges receipt and begins investigation
2. **Verification**: Team reproduces and confirms the vulnerability
3. **CVE Assignment**: Undisclosed CVE ID created if confirmed
4. **Fix Development**: Patch developed and tested
5. **Release**: Fix applied to latest version
6. **Public Disclosure**: Security advisory published via GitHub and newsletters

### Recognition

KrakenD offers non-monetary recognition:
- Credit in CVE ID (if desired)
- Public acknowledgment in release notes
- Addition to KrakenD Contributors GitHub organization
- Opportunity to engage with technical staff
- KrakenD merchandise

### Response Timeline

We aim to respond within:
- **Critical vulnerabilities**: 48 hours
- **High severity**: 1 week
- **Medium/Low severity**: 2 weeks

Patches will be released as soon as verified and tested.

## Security Considerations

### Local Execution Model

KrakenD MCP Server runs as a **local MCP server** via stdio transport:
- ✅ No network ports exposed
- ✅ No remote access
- ✅ Runs with your user permissions
- ✅ No data leaves your machine (except documentation downloads)

### Data Privacy

**What the MCP server accesses:**
- Local KrakenD configuration files (read-only by default)
- Cached documentation in `data/` directory
- Search index in `data/search/`

**What it does NOT access:**
- Your network traffic
- Other system files (outside working directory)
- Remote servers (except www.krakend.io for docs)
- Personal data

**What it sends externally:**
- Documentation downloads from `https://www.krakend.io/llms-full.txt` (once per 7 days)
- JSON schema downloads from `https://www.krakend.io/schema/` (on-demand for validation)

### File System Access

The MCP server operates with your user's file permissions:
- **Read access**: Configuration files in current working directory
- **Write access**: `data/` directory for cache and search index
- **No elevated privileges**: Runs as your user, no sudo/admin required

### Validation via Docker

When using Docker validation mode, the server:
- Mounts configuration files read-only
- Runs official KrakenD Docker images
- Does not expose ports
- Cleans up containers after validation

## Best Practices for Users

### 1. Keep Updated
```bash
# Check current version
./krakend-mcp-server --version

# Update to latest
cd /path/to/mcp-server
git pull
./scripts/build.sh --platform=darwin-arm64
```

### 2. Verify Docker Images
If using Docker validation:
```bash
# Pull official images only
docker pull krakend:latest
docker pull krakend/krakend-ee:latest

# Verify image signatures (recommended)
docker trust inspect --pretty krakend:latest
```

### 3. Review Configurations Before Deployment
The AI assistant helps create configurations, but **always review**:
- Authentication settings
- CORS policies
- Rate limiting
- Debug endpoints (disable in production)
- Security headers

### 4. Use in Trusted Environments
- Only run in directories you trust
- Avoid running in directories with sensitive data
- Review any custom skills or agents before installing

### 5. Monitor File Access
The MCP server logs to stderr:
```bash
# Monitor what files are being accessed
./krakend-mcp-server 2> mcp-server.log
```

### 6. Limit Permissions
On Unix systems, you can restrict file access:
```bash
# Make configs read-only
chmod 400 krakend.json

# Restrict data directory
chmod 700 data/
```

## Security Features

### Input Validation
- JSON syntax validation before processing
- Schema validation against official KrakenD schemas
- Path traversal prevention in file operations
- Safe handling of user-provided file paths

### Safe Defaults
- Read-only file operations by default
- No automatic file modifications without explicit user request
- Temporary files with restricted permissions (0600)
- Automatic cleanup of temporary files

### Error Handling
- No sensitive information in error messages
- Safe error propagation
- Logging to stderr only (no file logging of sensitive data)

## Known Limitations

### Not a Security Scanner
This tool validates **configuration correctness**, not security:
- Use the `audit_security` tool for security checks
- Always review security implications of your config
- Consider professional security audits for production configs

### Local Trust Model
The MCP server trusts the local environment:
- No authentication between AI client and server
- Assumes single-user local machine
- Not designed for multi-tenant or remote access

### Dependencies
Security of dependencies is managed via:
- Go module verification
- Regular dependency updates
- Automated security scanning in CI (planned)

## Responsible Disclosure

We follow responsible disclosure practices:
1. Report received and acknowledged
2. Issue verified and severity assessed
3. Fix developed and tested
4. Security advisory published
5. Credit given to reporter (if desired)

## Security Resources

- **KrakenD Security Documentation**: https://www.krakend.io/docs/authorization/
- **MCP Security Guidelines**: https://modelcontextprotocol.io/docs/security
- **Go Security Releases**: https://go.dev/security/

## Questions?

For security-related questions that are not vulnerabilities:
- Open a [GitHub Discussion](https://github.com/krakend/mcp-server/discussions)
- Join [KrakenD Community](https://github.com/krakend/krakend-ce/discussions)

---

**Last Updated**: December 15, 2025
**Policy Version**: 1.0
