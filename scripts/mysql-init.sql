CREATE USER IF NOT EXISTS 'go_analytics_ro'@'%' IDENTIFIED BY 'go_analytics_ro';
GRANT SELECT ON go_agentery.* TO 'go_analytics_ro'@'%';
FLUSH PRIVILEGES;
