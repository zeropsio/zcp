# Medusa.js Ecommerce on Zerops

**EXPERIMENTAL**: Medusa.js ecommerce platform with multiple deployment variants.

## Deployment Variants

### Development (non-HA)
```
app.zerops.io/recipe/medusa-next-devel
```

### Production (HA)
```
app.zerops.io/recipe/medusa-next-prod
```

### Analog.js Variant
```
app.zerops.io/recipe/medusa-analog-devel
```

## Architecture
- Medusa.js backend
- Next.js or Analog.js storefront
- Multiple services depending on variant

## Gotchas
- **EXPERIMENTAL RECIPE** - may require additional configuration
- Multiple deployment variants for different use cases
- Complex multi-service ecommerce architecture
