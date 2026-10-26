# Run only through Rails runner inside the lab Redmine VM.
require 'json'
require 'securerandom'
plan = JSON.parse(File.read('/root/opi-project.json'))
credentials_path = '/root/opi-team-credentials.json'
credentials = File.exist?(credentials_path) ? JSON.parse(File.read(credentials_path)) : {}
members = [
  ['kochnev-so', 'С. О.', 'Кочнев', 'Руководитель проекта', true],
  ['kochnev-do', 'Д. О.', 'Кочнев', 'Специалист по ИС', false],
  ['korolev-im', 'И. М.', 'Королёв', 'DevOps-инженер', false]
]
ActiveRecord::Base.transaction do
  Setting.rest_api_enabled = '1'
  Setting.login_required = '1'
  Setting.self_registration = '0'
  project = Project.find_or_initialize_by(identifier: plan['identifier'])
  project.name = plan['name']
  project.description = plan['description']
  project.is_public = false
  project.save!
  project.enabled_module_names = %w[issue_tracking time_tracking news documents files wiki repository calendar gantt]
  all_permissions = Redmine::AccessControl.permissions.map(&:name)
  base = %i[view_issues add_issues edit_issues add_issue_notes view_private_notes save_queries view_gantt view_calendar log_time view_time_entries view_wiki_pages edit_wiki_pages view_files manage_files browse_repository view_changesets]
  roles = {}
  members.each do |login, first, last, role_name, admin|
    role = Role.find_or_initialize_by(name: role_name)
    role.permissions = (base + (admin ? %i[manage_project select_project_modules manage_members manage_versions manage_categories manage_issue_relations manage_subtasks set_issues_private set_own_issues_private manage_public_queries edit_time_entries] : %i[manage_subtasks])).select { |p| all_permissions.include?(p) }
    role.issues_visibility = 'all'
    role.save!
    roles[role_name] = role
    user = User.find_or_initialize_by(login: login)
    if user.new_record?
      password = SecureRandom.urlsafe_base64(20)
      user.firstname = first
      user.lastname = last
      # Reserved technical addresses for local accounts; these are not the students' real email addresses.
      user.mail = "#{login}@opi.invalid"
      user.password = password
      user.password_confirmation = password
      user.must_change_passwd = false
      user.language = 'ru'
      user.admin = admin
      user.status = User::STATUS_ACTIVE
      user.save!
      credentials[login] = {'password' => password, 'technical_email' => user.mail}
    end
    member = Member.find_or_initialize_by(project_id: project.id, user_id: user.id)
    member.roles = [role]
    member.save!
  end
  statuses = {}
  %w[Запланирована Назначена В\ работе На\ контроле Возвращена Закрыта Отменена].each_with_index do |name, i|
    st = IssueStatus.find_or_initialize_by(name: name)
    st.is_closed = %w[Закрыта Отменена].include?(name)
    st.position ||= IssueStatus.maximum(:position).to_i + i + 1
    st.save!
    statuses[name] = st
  end
  trackers = %w[Цель Разработка Конфигурирование Тестирование Документирование].map do |name|
    tracker = Tracker.find_or_initialize_by(name: name)
    tracker.default_status = statuses['Запланирована']
    tracker.save!
    tracker
  end
  project.trackers = trackers
  roles.each do |name, role|
    pairs = [['Запланирована','Назначена'],['Назначена','В работе'],['В работе','На контроле'],['Возвращена','В работе']]
    if name == 'Руководитель проекта'
      pairs += [['На контроле','Закрыта'],['На контроле','Возвращена'],['Закрыта','Возвращена']]
      pairs += statuses.keys.reject { |x| %w[Закрыта Отменена].include?(x) }.map { |x| [x,'Отменена'] }
    end
    trackers.each do |tracker|
      pairs.each do |from, to|
        WorkflowTransition.find_or_create_by!(role_id: role.id, tracker_id: tracker.id, old_status_id: statuses[from].id, new_status_id: statuses[to].id, author: false, assignee: false)
      end
    end
  end
  leader = User.find_by!(login: 'kochnev-so')
  priority = IssuePriority.default || IssuePriority.first
  issue = lambda do |subject, description, parent, tracker, assignee|
    item = Issue.find_or_initialize_by(project_id: project.id, subject: subject)
    if item.new_record?
      item.author = leader
      item.tracker = tracker
      item.status = statuses['Запланирована']
      item.priority = priority
      item.parent_issue_id = parent&.id
      item.assigned_to = assignee
      item.description = description
      item.save!
    end
    item
  end
  stage1 = issue.call('Этап I — организация разработки', 'Критерий приёмки: работающая VM; настроенный Redmine; роли, задачи и Git; подтверждения в отчёте.', nil, trackers[0], leader)
  [
    ['VM, Redmine и Git руководителя', 'Проверить гостевую ОС, вход, сервисы, сохранность данных и Git. Зафиксировать версии и команды.', 'kochnev-so'],
    ['Проект, участники и ролевая модель', 'Создать проект варианта 34, участников, права, трекеры и переходы статусов. Проверить права обычных пользователей.', 'kochnev-so'],
    ['Дерево целей и задач по ЛР1', 'Декомпозировать требования FR-01–FR-22 и связать их с проверками. Сроки и роли этапа II согласуются с командой.', 'kochnev-so'],
    ['Детальная установка Redmine и тема', 'Установка и настройка по зоне ответственности специалиста по ИС. Приложить собственные подтверждения выполненной работы.', 'kochnev-do'],
    ['Git Flow и четыре пользователя', 'Продемонстрировать feature/release/hotfix и действия четырёх пользователей по зоне ответственности DevOps. Использовать реальные подтверждения.', 'korolev-im'],
    ['Репозиторий GitHub руководителя', 'Клонировать репозиторий внутри VM, настроить авторство, подготовить коммит с исходниками и README. Отправку подтвердить отдельно.', 'kochnev-so'],
    ['Отчёт и доклад руководителя', 'Отчёт по требованиям методички с реальными снимками экрана; устный доклад на 3–5 минут. ФИО преподавателя и срок сдачи ещё не заданы.', 'kochnev-so']
  ].each do |title, desc, login|
    issue.call(title, desc, stage1, trackers[2], User.find_by!(login: login))
  end
  stage2 = issue.call('Этап II — реализация информационной системы', 'Реализация по ЛР1. Go, React/TypeScript и PostgreSQL. Внешние интеграции работают через согласованные учебные сервисы. Исполнители этапа II пока не назначены.', nil, trackers[0], nil)
  plan['groups'].each do |group|
    parent = issue.call(group['title'], 'Приёмка по дочерним требованиям и результатам испытаний.', stage2, trackers[0], nil)
    group['tasks'].each do |task|
      issue.call("#{task['id']} — #{task['title']}", "Требование ЛР1:\n#{task['requirement']}\n\nКритерий приёмки: сценарий реализован и проверен; ссылка на код и результат проверки приложены к задаче.", parent, trackers[1], nil)
    end
  end
  issue.call('Приёмочные испытания и нефункциональные требования', 'Проверить конкурентное резервирование, повторные платежи, возвраты, права, сохранность данных после перезапуска, резервное копирование и восстановление, HTTPS, 100 сессий и интерфейс от 360 px в Chromium, Firefox и WebKit. Не заявлять непроверенные результаты.', stage2, trackers[3], nil)
  issue.call('Документация запуска и эксплуатации', 'Описать установку, конфигурацию, учебные данные, миграции, резервные копии, восстановление, обновление и ограничения внешних интеграций.', stage2, trackers[4], nil)
end
File.write(credentials_path, JSON.pretty_generate(credentials), mode: 'w', perm: 0600)
puts JSON.generate(project: plan['identifier'], issues: Project.find_by!(identifier: plan['identifier']).issues.count, users: members.map(&:first), email_mode: 'local technical .invalid addresses; no email delivery')
