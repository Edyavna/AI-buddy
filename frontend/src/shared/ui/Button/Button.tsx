import React from 'react';
import clsx from 'clsx';
import { Spinner } from '../Spinner/Spinner';
import styles from './Button.module.css';

export interface ButtonProps extends React.ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'ghost' | 'danger';
  size?: 'sm' | 'md' | 'lg';
  isLoading?: boolean;
  fullWidth?: boolean;
  leftIcon?: React.ReactNode;
  rightIcon?: React.ReactNode;
}

export const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  (
    {
      children,
      variant = 'primary',
      size = 'md',
      isLoading = false,
      disabled = false,
      fullWidth = false,
      leftIcon,
      rightIcon,
      className,
      type = 'button',
      ...rest
    },
    ref
  ) => {
    const isDisabled = disabled || isLoading;

    return (
      <button
        ref={ref}
        type={type}
        disabled={isDisabled}
        aria-busy={isLoading}
        className={clsx(
          styles.button,
          styles[variant],
          styles[size],
          {
            [styles.loading]: isLoading,
            [styles.fullWidth]: fullWidth,
          },
          className
        )}
        {...rest}
      >
        {isLoading && (
          <span className={styles.spinnerWrapper}>
            <Spinner
              size={size === 'sm' ? 'sm' : 'md'}
              color={variant === 'primary' || variant === 'danger' ? 'white' : 'current'}
            />
          </span>
        )}
        <span className={clsx(styles.content, { [styles.hiddenContent]: isLoading })}>
          {leftIcon && <span className={styles.icon}>{leftIcon}</span>}
          <span>{children}</span>
          {rightIcon && <span className={styles.icon}>{rightIcon}</span>}
        </span>
      </button>
    );
  }
);

Button.displayName = 'Button';
