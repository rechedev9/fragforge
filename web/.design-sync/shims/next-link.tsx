// design-sync shim for `next/link` — renders a plain anchor so DS components
// that wrap navigation in <Link> bundle and render outside Next's runtime.
import * as React from 'react';

type LinkProps = {
  href?: string | { pathname?: string };
  children?: React.ReactNode;
  prefetch?: boolean;
  replace?: boolean;
  scroll?: boolean;
  shallow?: boolean;
} & Omit<React.AnchorHTMLAttributes<HTMLAnchorElement>, 'href'>;

const Link = React.forwardRef<HTMLAnchorElement, LinkProps>(function Link(
  { href, children, prefetch, replace, scroll, shallow, ...rest },
  ref,
) {
  const resolved = typeof href === 'string' ? href : href?.pathname ?? '#';
  return (
    <a ref={ref} href={resolved} {...rest}>
      {children}
    </a>
  );
});

export default Link;
