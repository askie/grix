class AppAgreementSectionData {
  const AppAgreementSectionData({
    required this.titleKey,
    this.paragraphKeys = const <String>[],
    this.bulletKeys = const <String>[],
  });

  final String titleKey;
  final List<String> paragraphKeys;
  final List<String> bulletKeys;
}

class AppAgreementContent {
  static const List<String> noticeKeys = <String>[
    'app_agreement_notice_item_1',
    'app_agreement_notice_item_2',
    'app_agreement_notice_item_3',
  ];

  static const List<AppAgreementSectionData> sections =
      <AppAgreementSectionData>[
    AppAgreementSectionData(
      titleKey: 'app_agreement_section_positioning_title',
      paragraphKeys: <String>[
        'app_agreement_section_positioning_body_1',
        'app_agreement_section_positioning_body_2',
      ],
    ),
    AppAgreementSectionData(
      titleKey: 'app_agreement_section_risk_title',
      paragraphKeys: <String>[
        'app_agreement_section_risk_body_1',
        'app_agreement_section_risk_body_2',
      ],
      bulletKeys: <String>[
        'app_agreement_section_risk_bullet_1',
        'app_agreement_section_risk_bullet_2',
        'app_agreement_section_risk_bullet_3',
        'app_agreement_section_risk_bullet_4',
        'app_agreement_section_risk_bullet_5',
      ],
    ),
    AppAgreementSectionData(
      titleKey: 'app_agreement_section_judgement_title',
      paragraphKeys: <String>[
        'app_agreement_section_judgement_body_1',
        'app_agreement_section_judgement_body_2',
      ],
      bulletKeys: <String>[
        'app_agreement_section_judgement_bullet_1',
        'app_agreement_section_judgement_bullet_2',
        'app_agreement_section_judgement_bullet_3',
      ],
    ),
    AppAgreementSectionData(
      titleKey: 'app_agreement_section_boundary_title',
      paragraphKeys: <String>[
        'app_agreement_section_boundary_body_1',
        'app_agreement_section_boundary_body_2',
        'app_agreement_section_boundary_body_3',
      ],
    ),
    AppAgreementSectionData(
      titleKey: 'app_agreement_section_update_title',
      paragraphKeys: <String>[
        'app_agreement_section_update_body_1',
        'app_agreement_section_update_body_2',
      ],
    ),
  ];
}
