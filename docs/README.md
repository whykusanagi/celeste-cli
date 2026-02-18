# Celeste CLI Documentation

This directory contains CLI-specific documentation for the Celeste terminal interface.

## Brand System Documentation

The complete **Celeste Brand System** documentation (brand guidelines, components, platforms, standards, governance) lives in the **corrupted-theme** npm package:

```
📦 ../corrupted-theme/docs/
├── brand/                              # Brand foundation
│   ├── BRAND_OVERVIEW.md
│   ├── DESIGN_TOKENS.md
│   ├── COLOR_SYSTEM.md
│   ├── TYPOGRAPHY.md
│   └── TRANSLATION_FAILURE_AESTHETIC.md
├── components/                         # Component specifications
│   ├── COMPONENT_LIBRARY.md
│   ├── GLASSMORPHISM.md
│   ├── INTERACTIVE_STATES.md
│   └── ANIMATION_GUIDELINES.md
├── platforms/                          # Platform guides
│   ├── WEB_IMPLEMENTATION.md
│   ├── CLI_IMPLEMENTATION.md
│   ├── NPM_PACKAGE.md
│   └── COMPONENT_MAPPING.md
├── standards/                          # Quality standards
│   ├── ACCESSIBILITY.md
│   ├── SPACING_SYSTEM.md
│   └── ANTI_PATTERNS.md
└── governance/                         # Governance & contribution
    ├── DESIGN_SYSTEM_GOVERNANCE.md
    ├── VERSION_MANAGEMENT.md
    └── CONTRIBUTION_GUIDELINES.md
```

**Total**: 18 documents, ~7,500+ lines of enterprise-grade brand documentation

## CLI-Specific Documentation

This folder contains:

- **CAPABILITIES.md** - What the CLI can do
- **ROUTING.md** - Command routing and structure
- **LLM_PROVIDERS.md** - Supported AI providers
- **PERSONALITY.md** - Celeste's AI personality
- **CORRUPTION_PHRASES.md** - CLI-specific corruption text
- **CHARACTER_LEVEL_CORRUPTION.md** - Character corruption implementation
- **STYLE_GUIDE.md** - CLI coding style
- **IMPLEMENTATION_VALIDATION.md** - CLI validation checklist
- **ROADMAP.md** - Future CLI features
- **FUTURE_WORK.md** - Planned improvements

## Why Separate?

The brand system documentation is maintained in `corrupted-theme` because:
1. **Source of Truth**: npm package is distributed, so docs should live there
2. **Cross-Platform**: Brand docs cover CLI + web + future platforms
3. **Clean Separation**: CLI repo stays focused on CLI implementation
4. **Easy Access**: Both projects can reference `../corrupted-theme/docs/`

## Local Development

To access brand docs during CLI development:

```bash
# From celeste-cli directory
cd ../corrupted-theme/docs/

# Or open directly
open ../corrupted-theme/docs/brand/BRAND_OVERVIEW.md
```

## Implementation Compliance

The CLI should adhere to:
- **Color System**: `../corrupted-theme/docs/brand/COLOR_SYSTEM.md`
- **Animation Timing**: `../corrupted-theme/docs/components/ANIMATION_GUIDELINES.md`
- **Corruption Rules**: `../corrupted-theme/docs/brand/TRANSLATION_FAILURE_AESTHETIC.md`
- **CLI Patterns**: `../corrupted-theme/docs/platforms/CLI_IMPLEMENTATION.md`

---

**Note**: The `corrupted-theme` directory is gitignored in this repo but expected to exist as a sibling directory for development.
